package integrations

import (
	"testing"
)

func TestDetectCronjob_Basic(t *testing.T) {
	labels := map[string]string{
		"swarm.cronjob.enable":       "true",
		"swarm.cronjob.schedule":     "0 */5 * * *",
		"swarm.cronjob.skip-running": "true",
		"swarm.cronjob.replicas":     "2",
	}

	result := detectCronjob(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Name != "swarm-cronjob" {
		t.Errorf("expected name 'swarm-cronjob', got %q", result.Name)
	}

	if !result.Enabled {
		t.Error("expected enabled=true")
	}

	if result.Schedule != "0 */5 * * *" {
		t.Errorf("unexpected schedule: %q", result.Schedule)
	}

	if !result.SkipRunning {
		t.Error("expected skipRunning=true")
	}

	if result.Replicas != 2 {
		t.Errorf("expected replicas=2, got %d", result.Replicas)
	}
}

func TestDetectCronjob_NoLabels(t *testing.T) {
	result := detectCronjob(map[string]string{
		"com.docker.stack.namespace": "mystack",
	})
	if result != nil {
		t.Error("expected nil for labels without swarm.cronjob prefix")
	}

	result = detectCronjob(map[string]string{})
	if result != nil {
		t.Error("expected nil for empty labels")
	}

	result = detectCronjob(nil)
	if result != nil {
		t.Error("expected nil for nil labels")
	}
}

func TestDetectCronjob_EnabledFalse(t *testing.T) {
	labels := map[string]string{
		"swarm.cronjob.enable":   "false",
		"swarm.cronjob.schedule": "0 0 * * *",
	}

	result := detectCronjob(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestDetectCronjob_ScheduleOnly(t *testing.T) {
	labels := map[string]string{
		"swarm.cronjob.schedule": "@hourly",
	}

	result := detectCronjob(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if !result.Enabled {
		t.Error("expected enabled=true (default when no explicit enable label)")
	}

	if result.Schedule != "@hourly" {
		t.Errorf("unexpected schedule: %q", result.Schedule)
	}

	if result.SkipRunning {
		t.Error("expected skipRunning=false (default)")
	}

	if result.Replicas != 0 {
		t.Errorf("expected replicas=0 (default), got %d", result.Replicas)
	}
}

func TestDetectCronjob_RegistryFields(t *testing.T) {
	labels := map[string]string{
		"swarm.cronjob.enable":         "true",
		"swarm.cronjob.schedule":       "0 */5 * * *",
		"swarm.cronjob.registry-auth":  "true",
		"swarm.cronjob.query-registry": "true",
	}

	result := detectCronjob(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if !result.RegistryAuth {
		t.Error("expected registryAuth=true")
	}

	if !result.QueryRegistry {
		t.Error("expected queryRegistry=true")
	}
}
