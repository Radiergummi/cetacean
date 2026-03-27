package integrations

import "testing"

func TestDetectShepherd_Basic(t *testing.T) {
	labels := map[string]string{
		"shepherd.enable":       "true",
		"shepherd.schedule":     "0 * * * *",
		"shepherd.image-filter": "myapp.*",
		"shepherd.latest":       "true",
		"shepherd.update-opts":  "--no-resolve-image",
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

	if result.Schedule != "0 * * * *" {
		t.Errorf("expected schedule '0 * * * *', got %q", result.Schedule)
	}

	if result.ImageFilter != "myapp.*" {
		t.Errorf("expected imageFilter 'myapp.*', got %q", result.ImageFilter)
	}

	if !result.Latest {
		t.Error("expected latest=true")
	}

	if result.UpdateOpts != "--no-resolve-image" {
		t.Errorf("expected updateOpts '--no-resolve-image', got %q", result.UpdateOpts)
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
		"shepherd.enable":   "false",
		"shepherd.schedule": "0 0 * * *",
	}

	result := detectShepherd(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Enabled {
		t.Error("expected enabled=false")
	}

	if result.Schedule != "0 0 * * *" {
		t.Errorf("expected schedule '0 0 * * *', got %q", result.Schedule)
	}
}

func TestDetectShepherd_EnableOnly(t *testing.T) {
	labels := map[string]string{
		"shepherd.enable": "true",
	}

	result := detectShepherd(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if !result.Enabled {
		t.Error("expected enabled=true")
	}

	if result.Schedule != "" {
		t.Errorf("expected empty schedule, got %q", result.Schedule)
	}

	if result.ImageFilter != "" {
		t.Errorf("expected empty imageFilter, got %q", result.ImageFilter)
	}

	if result.Latest {
		t.Error("expected latest=false")
	}

	if result.UpdateOpts != "" {
		t.Errorf("expected empty updateOpts, got %q", result.UpdateOpts)
	}
}
