package integrations

import (
	"testing"
	"time"
)

const docoSHA = "0123456789abcdef0123456789abcdef01234567"

func TestDetectDocoCD_Deployment(t *testing.T) {
	labels := map[string]string{
		"cd.doco.metadata.manager":      "doco-cd",
		"cd.doco.metadata.version":      "0.42.0",
		"cd.doco.deployment.name":       "shop",
		"cd.doco.deployment.timestamp":  "2026-10-27T09:30:00Z",
		"cd.doco.deployment.trigger":    "poll",
		"cd.doco.deployment.target.ref": "main",
		"cd.doco.deployment.target.sha": docoSHA,
		"cd.doco.source":                "git",
		"cd.doco.source.name":           "acme/shop",
		"cd.doco.source.url":            "https://github.com/acme/shop.git",
	}

	got := detectDocoCD(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Name != "doco-cd" || !got.Enabled {
		t.Errorf("Name = %q, Enabled = %v", got.Name, got.Enabled)
	}

	if got.Deployment != "shop" || got.Repository != "acme/shop" || got.Source != "git" {
		t.Errorf("unexpected identity fields: %+v", got)
	}

	if got.Ref != "main" || got.Commit != docoSHA || got.Trigger != "poll" {
		t.Errorf("unexpected revision fields: %+v", got)
	}

	if got.Version != "0.42.0" {
		t.Errorf("Version = %q", got.Version)
	}

	want := time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC)
	if got.DeployedAt == nil || !got.DeployedAt.Equal(want) {
		t.Errorf("DeployedAt = %v, want %v", got.DeployedAt, want)
	}

	if got.CommitURL != "https://github.com/acme/shop/commit/"+docoSHA {
		t.Errorf("CommitURL = %q", got.CommitURL)
	}
}

func TestDetectDocoCD_Job(t *testing.T) {
	labels := map[string]string{
		"cd.doco.job.enabled":        "true",
		"cd.doco.job.schedule":       "@every 1h",
		"cd.doco.job.execution_mode": "one_off",
		"cd.doco.job.skip_running":   "true",
		"cd.doco.job.last_run":       "2026-10-27T08:00:00Z",
		"cd.doco.job.next_run":       "not a time",
	}

	got := detectDocoCD(labels)
	if got == nil || got.Job == nil {
		t.Fatalf("expected a job, got %+v", got)
	}

	job := got.Job
	if !job.Enabled || job.Schedule != "@every 1h" || job.ExecutionMode != "one_off" {
		t.Errorf("unexpected job: %+v", job)
	}

	if job.SkipRunning == nil || !*job.SkipRunning {
		t.Errorf("SkipRunning = %v, want true", job.SkipRunning)
	}

	if job.LastRun == nil {
		t.Error("LastRun = nil, want a time")
	}

	if job.NextRun != nil {
		t.Errorf("NextRun = %v, want nil for an unparsable value", job.NextRun)
	}
}

func TestDetectDocoCD_NoJobWithoutJobLabels(t *testing.T) {
	got := detectDocoCD(map[string]string{"cd.doco.deployment.name": "shop"})
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Job != nil {
		t.Errorf("Job = %+v, want nil", got.Job)
	}
}

func TestDetectDocoCD_CertExpiry(t *testing.T) {
	got := detectDocoCD(map[string]string{
		"cd.doco.deployment.cert.expiry": "2027-01-01T00:00:00Z",
	})
	if got == nil || got.CertExpiry == nil {
		t.Fatalf("expected a cert expiry, got %+v", got)
	}
}

func TestDetectDocoCD_SourceURLCredentialsAreStripped(t *testing.T) {
	got := detectDocoCD(map[string]string{ //nolint:gosec // test data, not real credentials
		"cd.doco.source.url":            "https://oauth2:secret@gitlab.com/acme/shop.git",
		"cd.doco.deployment.target.sha": docoSHA,
	})
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.SourceURL != "https://gitlab.com/acme/shop.git" {
		t.Errorf("SourceURL = %q", got.SourceURL)
	}

	if got.CommitURL != "https://gitlab.com/acme/shop/-/commit/"+docoSHA {
		t.Errorf("CommitURL = %q", got.CommitURL)
	}
}

func TestDetectDocoCD_UnparsableSourceURLIsDropped(t *testing.T) {
	got := detectDocoCD(map[string]string{ //nolint:gosec // test data, not real credentials
		"cd.doco.source.url": "https://user:ab/cd@gitlab.com/acme/shop.git",
	})
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.SourceURL != "" {
		t.Errorf("SourceURL = %q, want it dropped", got.SourceURL)
	}
}

func TestDetectDocoCD_NoLabels(t *testing.T) {
	if got := detectDocoCD(map[string]string{"cd.other": "x"}); got != nil {
		t.Errorf("expected nil result, got %+v", got)
	}
}

func TestDocoCommitURL(t *testing.T) {
	tests := []struct {
		name, source, sha, want string
	}{
		{
			"github https",
			"https://github.com/acme/shop.git",
			docoSHA,
			"https://github.com/acme/shop/commit/" + docoSHA,
		},
		{
			"github scp",
			"git@github.com:acme/shop.git",
			docoSHA,
			"https://github.com/acme/shop/commit/" + docoSHA,
		},
		{
			"ssh scheme drops port",
			"ssh://git@gitlab.example.com:2222/acme/shop.git",
			docoSHA,
			"https://gitlab.example.com/acme/shop/-/commit/" + docoSHA,
		},
		{
			"gitlab subgroup",
			"https://gitlab.com/acme/team/shop",
			docoSHA,
			"https://gitlab.com/acme/team/shop/-/commit/" + docoSHA,
		},
		{
			"codeberg",
			"https://codeberg.org/acme/shop.git",
			docoSHA,
			"https://codeberg.org/acme/shop/commit/" + docoSHA,
		},
		{
			"forgejo keeps http port",
			"http://forgejo.lan:3000/acme/shop",
			docoSHA,
			"http://forgejo.lan:3000/acme/shop/commit/" + docoSHA,
		},
		{"unknown forge", "https://git.example.com/acme/shop.git", docoSHA, ""},
		{
			"self-hosted gitlab",
			"https://gitlab.example.com/acme/shop.git",
			docoSHA,
			"https://gitlab.example.com/acme/shop/-/commit/" + docoSHA,
		},
		{
			"forge name inside another label",
			"https://github-mirror.gitlab.corp/acme/shop.git",
			docoSHA,
			"",
		},
		{
			"uppercase sha",
			"https://github.com/acme/shop.git",
			"ABCDEF0",
			"https://github.com/acme/shop/commit/ABCDEF0",
		},
		{"not a sha", "https://github.com/acme/shop.git", "main", ""},
		{"no repository path", "https://github.com/", docoSHA, ""},
		{"oci reference", "ghcr.io/acme/shop:main", docoSHA, ""},
		{"empty", "", docoSHA, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := docoCommitURL(tt.source, tt.sha); got != tt.want {
				t.Errorf("docoCommitURL(%q, %q) = %q, want %q", tt.source, tt.sha, got, tt.want)
			}
		})
	}
}
