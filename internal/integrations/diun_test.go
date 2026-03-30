package integrations

import (
	"testing"
)

func TestDetectDiun_Basic(t *testing.T) {
	labels := map[string]string{
		"diun.enable":       "true",
		"diun.watch_repo":   "true",
		"diun.notify_on":    "new;update",
		"diun.max_tags":     "10",
		"diun.include_tags": `^v\d+\.\d+\.\d+$`,
		"diun.exclude_tags": `^latest$`,
		"diun.sort_tags":    "semver",
	}

	got := detectDiun(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Name != "diun" {
		t.Errorf("Name = %q, want %q", got.Name, "diun")
	}

	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}

	if !got.WatchRepo {
		t.Error("WatchRepo = false, want true")
	}

	if got.NotifyOn != "new;update" {
		t.Errorf("NotifyOn = %q, want %q", got.NotifyOn, "new;update")
	}

	if got.MaxTags != 10 {
		t.Errorf("MaxTags = %d, want 10", got.MaxTags)
	}

	if got.IncludeTags != `^v\d+\.\d+\.\d+$` {
		t.Errorf("IncludeTags = %q, want regex", got.IncludeTags)
	}

	if got.ExcludeTags != `^latest$` {
		t.Errorf("ExcludeTags = %q, want %q", got.ExcludeTags, `^latest$`)
	}

	if got.SortTags != "semver" {
		t.Errorf("SortTags = %q, want %q", got.SortTags, "semver")
	}
}

func TestDetectDiun_NoLabels(t *testing.T) {
	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"traefik.enable":             "true",
	}

	got := detectDiun(labels)
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestDetectDiun_EnabledFalse(t *testing.T) {
	labels := map[string]string{
		"diun.enable":    "false",
		"diun.notify_on": "new",
	}

	got := detectDiun(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestDetectDiun_Metadata(t *testing.T) {
	labels := map[string]string{
		"diun.enable":         "true",
		"diun.metadata.team":  "platform",
		"diun.metadata.env":   "production",
		"diun.metadata.owner": "alice",
	}

	got := detectDiun(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if len(got.Metadata) != 3 {
		t.Fatalf("Metadata len = %d, want 3", len(got.Metadata))
	}

	cases := map[string]string{
		"team":  "platform",
		"env":   "production",
		"owner": "alice",
	}

	for key, want := range cases {
		if got.Metadata[key] != want {
			t.Errorf("Metadata[%q] = %q, want %q", key, got.Metadata[key], want)
		}
	}
}

func TestDetectDiun_WatchRepoOnly(t *testing.T) {
	labels := map[string]string{
		"diun.watch_repo": "true",
	}

	got := detectDiun(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Name != "diun" {
		t.Errorf("Name = %q, want %q", got.Name, "diun")
	}

	// No explicit enable label — should default to true.
	if !got.Enabled {
		t.Error("Enabled = false, want true (default)")
	}

	if !got.WatchRepo {
		t.Error("WatchRepo = false, want true")
	}
}

func TestDetectDiun_ExtraFields(t *testing.T) {
	labels := map[string]string{
		"diun.enable":   "true",
		"diun.regopt":   "ghcr.io",
		"diun.hub_link": "https://hub.docker.com/r/myapp",
		"diun.platform": "linux/amd64",
	}

	got := detectDiun(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.RegOpt != "ghcr.io" {
		t.Errorf("RegOpt = %q, want %q", got.RegOpt, "ghcr.io")
	}

	if got.HubLink != "https://hub.docker.com/r/myapp" {
		t.Errorf("HubLink = %q, want %q", got.HubLink, "https://hub.docker.com/r/myapp")
	}

	if got.Platform != "linux/amd64" {
		t.Errorf("Platform = %q, want %q", got.Platform, "linux/amd64")
	}
}
