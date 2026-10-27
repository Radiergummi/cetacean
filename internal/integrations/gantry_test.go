package integrations

import "testing"

func TestDetectGantry_AllLabels(t *testing.T) {
	labels := map[string]string{
		"gantry.auth.config":            "/run/secrets/registry",
		"gantry.manifest.cmd":           "manifest",
		"gantry.manifest.options":       "--insecure",
		"gantry.rollback.on_failure":    "false",
		"gantry.rollback.options":       "--detach=false",
		"gantry.update.jobs":            "true",
		"gantry.update.options":         "--update-parallelism=2",
		"gantry.update.timeout_seconds": "300",
	}

	got := detectGantry(labels)
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Name != "gantry" {
		t.Errorf("Name = %q, want %q", got.Name, "gantry")
	}

	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}

	if got.AuthConfig != "/run/secrets/registry" {
		t.Errorf("AuthConfig = %q", got.AuthConfig)
	}

	if got.ManifestCmd != "manifest" {
		t.Errorf("ManifestCmd = %q", got.ManifestCmd)
	}

	if got.ManifestOptions != "--insecure" {
		t.Errorf("ManifestOptions = %q", got.ManifestOptions)
	}

	if got.RollbackOnFailure == nil || *got.RollbackOnFailure {
		t.Errorf("RollbackOnFailure = %v, want false", got.RollbackOnFailure)
	}

	if got.RollbackOptions != "--detach=false" {
		t.Errorf("RollbackOptions = %q", got.RollbackOptions)
	}

	if got.UpdateJobs == nil || !*got.UpdateJobs {
		t.Errorf("UpdateJobs = %v, want true", got.UpdateJobs)
	}

	if got.UpdateOptions != "--update-parallelism=2" {
		t.Errorf("UpdateOptions = %q", got.UpdateOptions)
	}

	if got.UpdateTimeoutSeconds == nil || *got.UpdateTimeoutSeconds != 300 {
		t.Errorf("UpdateTimeoutSeconds = %v, want 300", got.UpdateTimeoutSeconds)
	}
}

func TestDetectGantry_UnsetOverridesStayNil(t *testing.T) {
	got := detectGantry(map[string]string{"gantry.auth.config": "x"})
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.RollbackOnFailure != nil || got.UpdateJobs != nil || got.UpdateTimeoutSeconds != nil {
		t.Errorf("unset overrides should be nil, got %+v", got)
	}
}

func TestDetectGantry_ZeroTimeoutIsKept(t *testing.T) {
	got := detectGantry(map[string]string{"gantry.update.timeout_seconds": "0"})
	if got == nil || got.UpdateTimeoutSeconds == nil || *got.UpdateTimeoutSeconds != 0 {
		t.Errorf("UpdateTimeoutSeconds = %v, want 0", got)
	}
}

func TestDetectGantry_Excluded(t *testing.T) {
	got := detectGantry(map[string]string{"gantry.services.excluded": "true"})
	if got == nil {
		t.Fatal("expected non-nil result")
	}

	if got.Enabled {
		t.Error("Enabled = true, want false for an excluded service")
	}
}

func TestDetectGantry_NoLabels(t *testing.T) {
	if got := detectGantry(map[string]string{"shepherd.enable": "true"}); got != nil {
		t.Errorf("expected nil result, got %+v", got)
	}
}
