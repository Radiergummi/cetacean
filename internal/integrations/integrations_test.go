package integrations

import "testing"

func TestDetect_NoIntegrations(t *testing.T) {
	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"app.version":               "1.0",
	}
	result := Detect(labels)
	if len(result.Integrations) != 0 {
		t.Errorf("expected no integrations, got %d", len(result.Integrations))
	}
	if len(result.Remaining) != 2 {
		t.Errorf("expected 2 remaining labels, got %d", len(result.Remaining))
	}
}

func TestDetect_TraefikDetected(t *testing.T) {
	labels := map[string]string{
		"traefik.enable":                                     "true",
		"traefik.http.routers.web.rule":                      "Host(`example.com`)",
		"traefik.http.services.web.loadbalancer.server.port": "8080",
		"com.docker.stack.namespace":                         "mystack",
		"app.version":                                        "1.0",
	}
	result := Detect(labels)
	if len(result.Integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(result.Integrations))
	}
	if len(result.Remaining) != 2 {
		t.Errorf("expected 2 remaining labels, got %d: %v", len(result.Remaining), result.Remaining)
	}
	if _, ok := result.Remaining["traefik.enable"]; ok {
		t.Error("traefik labels should not be in remaining")
	}
}

func TestDetect_TCPLabelsConsumed(t *testing.T) {
	labels := map[string]string{
		"traefik.tcp.routers.db.rule": "HostSNI(`db.example.com`)",
		"app.version":                 "1.0",
	}
	result := Detect(labels)
	if len(result.Integrations) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(result.Integrations))
	}
	if len(result.Remaining) != 1 {
		t.Errorf("expected 1 remaining label, got %d: %v", len(result.Remaining), result.Remaining)
	}
	if _, ok := result.Remaining["traefik.tcp.routers.db.rule"]; ok {
		t.Error("traefik.tcp labels should not be in remaining")
	}
}
