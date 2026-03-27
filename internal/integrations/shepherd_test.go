package integrations

import "testing"

func TestDetectShepherd_Basic(t *testing.T) {
	labels := map[string]string{
		"shepherd.enable":      "true",
		"shepherd.auth.config": "myregistry",
	}

	result := detectShepherd(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Name != "shepherd" {
		t.Errorf("expected name 'shepherd', got %q", result.Name)
	}

	if !result.Enabled {
		t.Error("expected enabled=true")
	}

	if result.AuthConfig != "myregistry" {
		t.Errorf("expected authConfig 'myregistry', got %q", result.AuthConfig)
	}
}

func TestDetectShepherd_NoLabels(t *testing.T) {
	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"traefik.enable":             "true",
	}

	result := detectShepherd(labels)
	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestDetectShepherd_EnabledFalse(t *testing.T) {
	labels := map[string]string{
		"shepherd.enable": "false",
	}

	result := detectShepherd(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestDetectShepherd_AuthConfig(t *testing.T) {
	labels := map[string]string{
		"shepherd.enable":      "true",
		"shepherd.auth.config": "ghcr.io",
	}

	result := detectShepherd(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if !result.Enabled {
		t.Error("expected enabled=true")
	}

	if result.AuthConfig != "ghcr.io" {
		t.Errorf("expected authConfig 'ghcr.io', got %q", result.AuthConfig)
	}
}
