package integrations

import (
	"reflect"
	"testing"
)

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

func TestDetect_ReportsClaimedLabels(t *testing.T) {
	labels := map[string]string{
		"Traefik.http.routers.web.rule": "Host(`example.com`)",
		"caddy":                         "example.com",
		"caddy_ingress_network":         "proxy",
		"gantry.update.jobs":            "true",
		"gantry.auth.config":            "x",
		"com.docker.stack.namespace":    "shop",
	}

	want := map[string][]string{
		"traefik": {"Traefik.http.routers.web.rule"},
		"caddy":   {"caddy"},
		"gantry":  {"gantry.auth.config", "gantry.update.jobs"},
	}

	got := map[string][]string{}

	for _, detected := range Detect(labels) {
		switch d := detected.(type) {
		case *TraefikIntegration:
			got[d.Name] = d.Labels
		case *CaddyIntegration:
			got[d.Name] = d.Labels
		case *GantryIntegration:
			got[d.Name] = d.Labels
		default:
			t.Errorf("unexpected integration %T", d)
		}
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("claimed labels = %v, want %v", got, want)
	}
}
