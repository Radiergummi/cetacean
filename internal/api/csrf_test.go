package api

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
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

	h := newTestHandlers(t, withCache(c), withWriteClient(wc))
	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	t.Cleanup(b.Close)
	fsys := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	spa := NewSPAHandler(fs.FS(fsys), "")

	var cors *CORSConfig
	if len(origins) > 0 {
		cors = &CORSConfig{AllowedOrigins: origins}
	}

	return NewRouter(RouterConfig{
		Handlers:          h,
		Broadcaster:       b,
		SPA:               spa,
		OpenAPISpec:       []byte("openapi: '3.1.0'"),
		EnableSelfMetrics: true,
		AuthProvider:      &auth.NoneProvider{},
		CORS:              cors,
	})
}

// restartRequest builds a POST that reaches a real write handler, so a request
// the protection lets through is visibly served rather than merely not-403.
func restartRequest() *http.Request {
	req := httptest.NewRequest("POST", "/services/svc1/restart", nil)
	req.Header.Set("Accept", "application/json")
	return req
}

// TestCrossSiteWriteIsRefused fails if the cross-origin protection is absent
// from the router chain: without it the restart is simply performed.
func TestCrossSiteWriteIsRefused(t *testing.T) {
	router := newCSRFTestRouter(t)

	req := restartRequest()
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}

// TestCrossSiteRefusalIsProblemDetails fails if the stdlib's plain-text 403 is
// left in place instead of a deny handler writing this API's error shape.
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

// TestCrossSiteWriteFromConfiguredOriginIsAllowed fails if the CORS origin list
// is not mirrored into the protection's trusted origins. That mismatch is the
// failure this item exists to prevent: an origin CORS admits, CSRF refuses.
func TestCrossSiteWriteFromConfiguredOriginIsAllowed(t *testing.T) {
	router := newCSRFTestRouter(t, "https://dashboard.test")

	req := restartRequest()
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://dashboard.test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

// TestWildcardCORSDoesNotTrustEveryOrigin pins the ruling that "*" cannot be
// mirrored: it is not a valid origin, and treating it as "trust everyone" would
// disable the protection through a setting that says nothing about CSRF.
func TestWildcardCORSDoesNotTrustEveryOrigin(t *testing.T) {
	router := newCSRFTestRouter(t, "*")

	req := restartRequest()
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}

// TestNonBrowserWriteIsAllowed fails if the protection is applied to clients
// that send neither Fetch Metadata nor Origin — curl, the Docker CLI, and every
// MCP client that is not a browser.
func TestNonBrowserWriteIsAllowed(t *testing.T) {
	router := newCSRFTestRouter(t)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, restartRequest())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

// TestSameOriginWriteIsAllowed fails if the dashboard's own writes are refused.
func TestSameOriginWriteIsAllowed(t *testing.T) {
	router := newCSRFTestRouter(t)

	req := restartRequest()
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

// TestCrossSiteReadIsAllowed fails if the protection is applied to safe
// methods, which would break every cross-origin read the CORS config permits.
func TestCrossSiteReadIsAllowed(t *testing.T) {
	router := newCSRFTestRouter(t, "https://dashboard.test")

	req := httptest.NewRequest("GET", "/services/svc1", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://dashboard.test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

// TestCrossSiteRequestToAuthRouteIsRefused pins that the auth routes are behind
// the same protection as everything else. They are exempt from authentication —
// auth.isExempt lists /auth/* — and it would be easy to read that exemption as
// covering this check too. It does not: the protection runs before auth, on
// every path. This is where OIDC's own logout wrapper went; the check no longer
// knows which route it is refusing, so it is pinned at the router.
func TestCrossSiteRequestToAuthRouteIsRefused(t *testing.T) {
	router := newCSRFTestRouter(t)

	req := httptest.NewRequest("POST", "/auth/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", rec.Code, rec.Body.String())
	}
}
