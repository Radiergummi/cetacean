package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func hostGuardRouter(t *testing.T, opts ...routerOption) http.Handler {
	t.Helper()

	return newTestRouterWithConfig(t, append([]routerOption{func(cfg *RouterConfig) {
		cfg.AllowedHosts = nil
		cfg.PublicURL = "https://swarm.example.com"
	}}, opts...))
}

func TestHostGuard_RefusesAForeignNameWith421(t *testing.T) {
	router := hostGuardRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.Host = "rebind.attacker.example:9000"
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d, want 421", rec.Code)
	}

	var problem ProblemDetail
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := "/api/errors/CSR002"; problem.Type != want {
		t.Errorf("type = %q, want %q", problem.Type, want)
	}
	for _, want := range []string{"rebind.attacker.example", "server.allowed_hosts", "server.public_url"} {
		if !strings.Contains(problem.Detail, want) {
			t.Errorf("detail %q does not name %s", problem.Detail, want)
		}
	}
}

func TestHostGuard_AdmitsOurOwnNames(t *testing.T) {
	router := hostGuardRouter(t, func(cfg *RouterConfig) {
		cfg.AllowedHosts = []string{"cetacean.tailnet.ts.net"}
	})

	for _, host := range []string{
		"swarm.example.com",
		"SWARM.example.com:443",
		"swarm.example.com.",
		"Cetacean.Tailnet.ts.net",
		"localhost:9000",
		"127.0.0.1:9000",
		"[::1]:9000",
		"10.0.0.5:9000",
		"[fd00::5]",
	} {
		req := httptest.NewRequest(http.MethodGet, "/services", nil)
		req.Host = host
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Host %q: status = %d, want 200", host, rec.Code)
		}
	}
}

func TestHostGuard_ProbesAnswerAnyHost(t *testing.T) {
	router := hostGuardRouter(t)

	for _, path := range []string{
		"/-/health", "/-/ready", "/-/metrics", "/-/health/", "/-/./ready", "//-/metrics",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "cetacean_cetacean:9000"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code == http.StatusMisdirectedRequest {
			t.Errorf("GET %s: refused a probe by its Host", path)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/-/resync", nil)
	req.Host = "cetacean_cetacean:9000"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("POST /-/resync: status = %d, want 421", rec.Code)
	}
}

func TestHostGuard_ATrustedProxyHasRoutedAlready(t *testing.T) {
	router := hostGuardRouter(t, func(cfg *RouterConfig) {
		cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	})

	for _, tc := range []struct {
		remote string
		want   int
	}{
		{"10.0.0.2:41000", http.StatusOK},
		{"192.0.2.7:41000", http.StatusMisdirectedRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, "/services", nil)
		req.RemoteAddr = tc.remote
		req.Host = "cetacean:9000"
		req.Header.Set("X-Forwarded-Host", "swarm.example.com")
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != tc.want {
			t.Errorf("peer %s: status = %d, want %d", tc.remote, rec.Code, tc.want)
		}
	}
}

func TestHostGuard_CoversMCP(t *testing.T) {
	router := hostGuardRouter(t, func(cfg *RouterConfig) {
		cfg.MCPHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Host = "rebind.attacker.example"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMisdirectedRequest {
		t.Errorf("status = %d, want 421", rec.Code)
	}
}
