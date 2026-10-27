package integrations

import "testing"

func TestDetectDeploymentAction_Provenance(t *testing.T) {
	labels := map[string]string{
		"com.matchory.deployment.version": "1.4.0",
		"com.matchory.deployment.commit":  docoSHA,
		"com.matchory.deployment.ref":     "refs/heads/main",
		"com.matchory.deployment.run-url": "https://github.com/acme/shop/actions/runs/123",
		"com.matchory.deployment.actor":   "octocat",
		"com.docker.stack.namespace":      "shop",
	}

	got := detectDeploymentAction(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Name != "docker-swarm-deployment-action" || !got.Enabled || len(got.Labels) != 5 {
		t.Errorf("Name = %q, Enabled = %v, Labels = %v", got.Name, got.Enabled, got.Labels)
	}

	if got.Version != "1.4.0" || got.Ref != "refs/heads/main" || got.Actor != "octocat" {
		t.Errorf("unexpected fields: %+v", got)
	}

	if got.Repository != "acme/shop" || got.RunURL != labels["com.matchory.deployment.run-url"] {
		t.Errorf("Repository = %q, RunURL = %q", got.Repository, got.RunURL)
	}

	if got.RepositoryURL != "https://github.com/acme/shop" {
		t.Errorf("RepositoryURL = %q", got.RepositoryURL)
	}

	if got.CommitURL != "https://github.com/acme/shop/commit/"+docoSHA {
		t.Errorf("CommitURL = %q", got.CommitURL)
	}
}

func TestDetectDeploymentAction_EnterpriseServer(t *testing.T) {
	got := detectDeploymentAction(map[string]string{
		"com.matchory.deployment.commit":  docoSHA,
		"com.matchory.deployment.run-url": "https://git.example.com:8443/acme/shop/actions/runs/7",
	})

	if got.CommitURL != "https://git.example.com:8443/acme/shop/commit/"+docoSHA {
		t.Errorf("CommitURL = %q", got.CommitURL)
	}
}

func TestDetectDeploymentAction_UnrecognisedRunURL(t *testing.T) {
	for _, runURL := range []string{
		"javascript:alert(1)",
		"https://user:token@github.com/acme/shop/actions/runs/1",
		"https://github.com/acme/shop/actions/runs/1/attempts/2",
		"https://github.com/acme/actions/runs/1",
		"https://github.com#x/acme/shop/actions/runs/1",
		"https://github.com/acme/shop?x=/actions/runs/1",
	} {
		got := detectDeploymentAction(map[string]string{
			"com.matchory.deployment.commit":  docoSHA,
			"com.matchory.deployment.run-url": runURL,
		})

		if got.RunURL != "" || got.Repository != "" || got.RepositoryURL != "" ||
			got.CommitURL != "" {
			t.Errorf("%q: RunURL = %q, Repository = %q, CommitURL = %q",
				runURL, got.RunURL, got.Repository, got.CommitURL)
		}
	}
}

func TestDetectDeploymentAction_NoCommitLinkWithoutSHA(t *testing.T) {
	got := detectDeploymentAction(map[string]string{
		"com.matchory.deployment.commit":  "not-a-sha",
		"com.matchory.deployment.run-url": "https://github.com/acme/shop/actions/runs/1",
	})

	if got.CommitURL != "" {
		t.Errorf("CommitURL = %q", got.CommitURL)
	}
}

func TestDetectDeploymentAction_Overrides(t *testing.T) {
	got := detectDeploymentAction(map[string]string{
		"com.matchory.deployment.monitor":               "False",
		"com.matchory.deployment.monitor-timeout":       "900",
		"com.matchory.deployment.health-check-warnings": "TRUE",
	})

	if got.Monitor == nil || *got.Monitor {
		t.Errorf("Monitor = %v", got.Monitor)
	}

	if got.MonitorTimeoutSeconds == nil || *got.MonitorTimeoutSeconds != 900 {
		t.Errorf("MonitorTimeoutSeconds = %v", got.MonitorTimeoutSeconds)
	}

	if got.HealthCheckWarnings == nil || !*got.HealthCheckWarnings {
		t.Errorf("HealthCheckWarnings = %v", got.HealthCheckWarnings)
	}
}

func TestDetectDeploymentAction_InvalidOverridesAreUnset(t *testing.T) {
	for _, v := range []string{"yes", "0", "+5", "-5", "5.9", "10s", ""} {
		got := detectDeploymentAction(map[string]string{
			"com.matchory.deployment.monitor":         v,
			"com.matchory.deployment.monitor-timeout": v,
		})

		if got.Monitor != nil || got.MonitorTimeoutSeconds != nil {
			t.Errorf(
				"%q: Monitor = %v, MonitorTimeoutSeconds = %v",
				v,
				got.Monitor,
				got.MonitorTimeoutSeconds,
			)
		}
	}
}

func TestDetectDeploymentAction_NoLabels(t *testing.T) {
	if got := detectDeploymentAction(map[string]string{"com.matchory.other": "x"}); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
