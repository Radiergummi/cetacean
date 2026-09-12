package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
)

// newCSRFTestRouter builds the assembled router around a service that can be
// restarted, with the given CORS origins. Every test here drives the composed
// stack rather than the middleware alone: the whole point of the item is where
// the check sits relative to cors and auth, which a unit test cannot see.
func newCSRFTestRouter(t testing.TB, origins ...string) http.Handler {
	t.Helper()

	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			restartServiceFn: func(_ context.Context, id string) (swarm.Service, error) {
				return replicatedService(id), nil
			},
		},
	}

	return newTestRouterWithConfig(
		t,
		[]routerOption{withCORS(origins...)},
		withCache(c),
		withWriteClient(wc),
	)
}

// restartRequest builds a POST that reaches a real write handler, so a request
// the protection lets through is visibly served rather than merely not-403.
func restartRequest() *http.Request {
	req := httptest.NewRequest("POST", "/services/svc1/restart", nil)
	req.Header.Set("Accept", "application/json")
	return req
}

// TestCrossOriginProtection drives the assembled router for each way a request
// can arrive at it. The rows are the rules, and every one of them fails if the
// chain entry is removed, the CORS mirroring breaks, or safe methods stop
// passing.
func TestCrossOriginProtection(t *testing.T) {
	tests := []struct {
		name      string
		origins   []string
		method    string
		path      string
		fetchSite string
		origin    string
		want      int
	}{
		// Without the chain entry the restart is simply performed.
		{
			name:      "cross-site write is refused",
			method:    "POST",
			path:      "/services/svc1/restart",
			fetchSite: "cross-site",
			origin:    "https://evil.test",
			want:      http.StatusForbidden,
		},

		// The mismatch this item exists to prevent: an origin CORS admits,
		// CSRF refuses.
		{
			name:      "cross-site write from a configured origin is allowed",
			origins:   []string{"https://dashboard.test"},
			method:    "POST",
			path:      "/services/svc1/restart",
			fetchSite: "cross-site",
			origin:    "https://dashboard.test",
			want:      http.StatusOK,
		},

		// "*" cannot be mirrored: it is not a valid origin, and treating it as
		// "trust everyone" would disable the protection through a setting that
		// says nothing about CSRF.
		{
			name:      "a wildcard CORS list trusts no origin",
			origins:   []string{"*"},
			method:    "POST",
			path:      "/services/svc1/restart",
			fetchSite: "cross-site",
			origin:    "https://evil.test",
			want:      http.StatusForbidden,
		},

		// Clients that send neither Fetch Metadata nor Origin — curl, the
		// Docker CLI, every MCP client that is not a browser.
		{
			name:   "non-browser write is allowed",
			method: "POST",
			path:   "/services/svc1/restart",
			want:   http.StatusOK,
		},

		{
			name:      "same-origin write is allowed",
			method:    "POST",
			path:      "/services/svc1/restart",
			fetchSite: "same-origin",
			want:      http.StatusOK,
		},

		// Applying the check to safe methods would break every cross-origin
		// read the CORS config permits.
		{
			name:      "cross-site read is allowed",
			origins:   []string{"https://dashboard.test"},
			method:    "GET",
			path:      "/services/svc1",
			fetchSite: "cross-site",
			origin:    "https://dashboard.test",
			want:      http.StatusOK,
		},

		// The auth routes are exempt from authentication — auth.isExempt lists
		// /auth/* — and it would be easy to read that exemption as covering
		// this check too. It does not: the entry sits above auth, on every
		// path, which is why OIDC's own logout wrapper could go.
		{
			name:      "cross-site write to an auth route is refused",
			method:    "POST",
			path:      "/auth/logout",
			fetchSite: "cross-site",
			want:      http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Accept", "application/json")

			if tt.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tt.fetchSite)
			}

			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			newCSRFTestRouter(t, tt.origins...).ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

// TestCrossSiteRefusalIsProblemDetails fails if the stdlib's plain-text 403 is
// left in place instead of Check plus this API's own error shape.
func TestCrossSiteRefusalIsProblemDetails(t *testing.T) {
	router := newCSRFTestRouter(t)

	req := restartRequest()
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json; body: %s",
			ct, rec.Body.String())
	}

	var problem ProblemDetail
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if want := "/api/errors/CSR001"; problem.Type != want {
		t.Errorf("type = %q, want %q", problem.Type, want)
	}
	if problem.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", problem.Status)
	}
}

// Covers the fallback a pre-2023 browser takes: no Sec-Fetch-Site, so the stdlib
// compares Origin against r.Host. Behind a proxy that rewrites Host, the
// dashboard's own writes arrive with the public origin and an internal Host, and
// only server.public_url tells those from a stranger's.
func TestPublicURLIsTrusted(t *testing.T) {
	handler := crossOriginProtection(nil, "https://cetacean.example")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	tests := []struct {
		name   string
		origin string
		want   int
	}{
		{
			name:   "the public origin is trusted",
			origin: "https://cetacean.example",
			want:   http.StatusNoContent,
		},
		{
			name:   "any other origin is still refused",
			origin: "https://evil.test",
			want:   http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/services/svc1/restart", nil)
			req.Host = "cetacean.internal:9000"
			req.Header.Set("Origin", tt.origin)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}
