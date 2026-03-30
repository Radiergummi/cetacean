package integrations

import (
	"testing"
)

func TestDetectTraefik_BasicRouter(t *testing.T) {
	labels := map[string]string{
		"traefik.enable":                                                  "true",
		"traefik.http.routers.myapp.rule":                                 "Host(`myapp.example.com`)",
		"traefik.http.routers.myapp.entrypoints":                          "websecure",
		"traefik.http.routers.myapp.middlewares":                          "auth@docker,compress@docker",
		"traefik.http.routers.myapp.service":                              "myapp",
		"traefik.http.routers.myapp.priority":                             "100",
		"traefik.http.routers.myapp.tls":                                  "true",
		"traefik.http.routers.myapp.tls.certresolver":                     "letsencrypt",
		"traefik.http.services.myapp.loadbalancer.server.port":            "8080",
		"traefik.http.services.myapp.loadbalancer.server.scheme":          "http",
		"traefik.http.middlewares.auth.basicauth.users":                   "admin:$$2y$$...",
		"traefik.http.middlewares.compress.compress.excludedcontenttypes": "text/event-stream",
	}

	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if !result.Enabled {
		t.Error("expected enabled=true")
	}

	if len(result.Routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(result.Routers))
	}

	r := result.Routers[0]
	if r.Name != "myapp" {
		t.Errorf("expected router name 'myapp', got %q", r.Name)
	}
	if r.Rule != "Host(`myapp.example.com`)" {
		t.Errorf("unexpected rule: %q", r.Rule)
	}
	if len(r.Entrypoints) != 1 || r.Entrypoints[0] != "websecure" {
		t.Errorf("unexpected entrypoints: %v", r.Entrypoints)
	}
	if len(r.Middlewares) != 2 || r.Middlewares[0] != "auth@docker" ||
		r.Middlewares[1] != "compress@docker" {
		t.Errorf("unexpected middlewares: %v", r.Middlewares)
	}
	if r.Service != "myapp" {
		t.Errorf("unexpected service: %q", r.Service)
	}
	if r.Priority != 100 {
		t.Errorf("expected priority 100, got %d", r.Priority)
	}
	if r.TLS == nil {
		t.Fatal("expected TLS to be set")
	}
	if r.TLS.CertResolver != "letsencrypt" {
		t.Errorf("unexpected certresolver: %q", r.TLS.CertResolver)
	}

	if len(result.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result.Services))
	}

	svc := result.Services[0]
	if svc.Name != "myapp" {
		t.Errorf("expected service name 'myapp', got %q", svc.Name)
	}
	if svc.Port != 8080 {
		t.Errorf("expected port 8080, got %d", svc.Port)
	}
	if svc.Scheme != "http" {
		t.Errorf("expected scheme 'http', got %q", svc.Scheme)
	}

	if len(result.Middlewares) != 2 {
		t.Fatalf("expected 2 middlewares, got %d", len(result.Middlewares))
	}

	// Sorted by name: auth, compress.
	if result.Middlewares[0].Name != "auth" || result.Middlewares[0].Type != "basicauth" {
		t.Errorf("unexpected first middleware: %+v", result.Middlewares[0])
	}
	if result.Middlewares[1].Name != "compress" || result.Middlewares[1].Type != "compress" {
		t.Errorf("unexpected second middleware: %+v", result.Middlewares[1])
	}
}

func TestDetectTraefik_NoLabels(t *testing.T) {
	result := detectTraefik(map[string]string{
		"com.docker.stack.namespace": "mystack",
	})
	if result != nil {
		t.Error("expected nil for labels without traefik prefix")
	}

	result = detectTraefik(map[string]string{})
	if result != nil {
		t.Error("expected nil for empty labels")
	}

	result = detectTraefik(nil)
	if result != nil {
		t.Error("expected nil for nil labels")
	}
}

func TestDetectTraefik_EnabledFalse(t *testing.T) {
	result := detectTraefik(map[string]string{
		"traefik.enable": "false",
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestDetectTraefik_MultipleRouters(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.web.rule":        "Host(`web.example.com`)",
		"traefik.http.routers.web.entrypoints": "websecure",
		"traefik.http.routers.api.rule":        "Host(`api.example.com`)",
		"traefik.http.routers.api.entrypoints": "websecure",
	}

	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Routers) != 2 {
		t.Fatalf("expected 2 routers, got %d", len(result.Routers))
	}

	// Sorted by name: api before web.
	if result.Routers[0].Name != "api" {
		t.Errorf("expected first router 'api', got %q", result.Routers[0].Name)
	}
	if result.Routers[1].Name != "web" {
		t.Errorf("expected second router 'web', got %q", result.Routers[1].Name)
	}
}

func TestDetectTraefik_TLSDomains(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.myapp.rule":                "Host(`example.com`)",
		"traefik.http.routers.myapp.tls.domains[0].main": "example.com",
		"traefik.http.routers.myapp.tls.domains[0].sans": "www.example.com,api.example.com",
		"traefik.http.routers.myapp.tls.domains[1].main": "other.com",
	}

	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	r := result.Routers[0]
	if r.TLS == nil {
		t.Fatal("expected TLS to be set")
	}

	if len(r.TLS.Domains) != 2 {
		t.Fatalf("expected 2 TLS domains, got %d", len(r.TLS.Domains))
	}

	if r.TLS.Domains[0].Main != "example.com" {
		t.Errorf("unexpected domain[0].main: %q", r.TLS.Domains[0].Main)
	}
	if len(r.TLS.Domains[0].SANs) != 2 || r.TLS.Domains[0].SANs[0] != "www.example.com" {
		t.Errorf("unexpected domain[0].sans: %v", r.TLS.Domains[0].SANs)
	}
	if r.TLS.Domains[1].Main != "other.com" {
		t.Errorf("unexpected domain[1].main: %q", r.TLS.Domains[1].Main)
	}
	if len(r.TLS.Domains[1].SANs) != 0 {
		t.Errorf("expected no SANs for domain[1], got %v", r.TLS.Domains[1].SANs)
	}
}

func TestDetectTraefik_MiddlewareConfig(t *testing.T) {
	labels := map[string]string{
		"traefik.http.middlewares.ratelimit.ratelimit.average":    "100",
		"traefik.http.middlewares.ratelimit.ratelimit.burst":      "50",
		"traefik.http.middlewares.redirect.redirectscheme.scheme": "https",
	}

	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Middlewares) != 2 {
		t.Fatalf("expected 2 middlewares, got %d", len(result.Middlewares))
	}

	// Sorted: ratelimit, redirect.
	rl := result.Middlewares[0]
	if rl.Name != "ratelimit" || rl.Type != "ratelimit" {
		t.Errorf("unexpected middleware: %+v", rl)
	}
	if rl.Config["average"] != "100" || rl.Config["burst"] != "50" {
		t.Errorf("unexpected config: %v", rl.Config)
	}

	rd := result.Middlewares[1]
	if rd.Name != "redirect" || rd.Type != "redirectscheme" {
		t.Errorf("unexpected middleware: %+v", rd)
	}
	if rd.Config["scheme"] != "https" {
		t.Errorf("unexpected config: %v", rd.Config)
	}
}

func TestDetectTraefik_TCPLabelsConsumed(t *testing.T) {
	labels := map[string]string{
		"traefik.tcp.routers.mytcp.rule":        "HostSNI(`tcp.example.com`)",
		"traefik.tcp.routers.mytcp.entrypoints": "tcp",
	}

	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected non-nil result for TCP labels")
	}

	if len(result.Routers) != 0 {
		t.Errorf("expected 0 HTTP routers, got %d", len(result.Routers))
	}
	if len(result.Services) != 0 {
		t.Errorf("expected 0 HTTP services, got %d", len(result.Services))
	}
}

func TestDetectTraefik_RouterWithoutService(t *testing.T) {
	labels := map[string]string{
		"traefik.http.routers.myapp.rule":        "Host(`myapp.example.com`)",
		"traefik.http.routers.myapp.entrypoints": "websecure",
	}

	result := detectTraefik(labels)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Routers) != 1 {
		t.Fatalf("expected 1 router, got %d", len(result.Routers))
	}
	if len(result.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(result.Services))
	}
}
