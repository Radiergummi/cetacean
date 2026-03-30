package integrations

import "testing"

func TestDetect_NoIntegrations(t *testing.T) {
	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"app.version":                "1.0",
	}
	result := Detect(labels)
	if len(result) != 0 {
		t.Errorf("expected no integrations, got %d", len(result))
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
	if len(result) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(result))
	}
}

func TestDetect_TCPLabelsConsumed(t *testing.T) {
	labels := map[string]string{
		"traefik.tcp.routers.db.rule": "HostSNI(`db.example.com`)",
		"app.version":                 "1.0",
	}
	result := Detect(labels)
	if len(result) != 1 {
		t.Fatalf("expected 1 integration, got %d", len(result))
	}
}
