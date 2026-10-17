package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

// The SPA has no route under /-/, so a path there that nothing serves is no
// resource whatever the client accepts, not the dashboard shell.
func TestUnservedMetaPathIsNotFound(t *testing.T) {
	handler, _ := newRouter(testRouterConfig(t, []routerOption{
		func(cfg *RouterConfig) { cfg.EnableSelfMetrics = false },
	}))

	for _, path := range []string{"/-/nope", "/-/metrics", "/-/health/extra"} {
		for _, accept := range []string{"", "*/*", "text/html"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s (Accept %q) = %d, want 404", path, accept, rec.Code)
			}
		}
	}
}

// A meta route asked with a method it does not serve says which it does.
func TestMetaPathWithTheWrongMethodIsNotAllowed(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/405-sends-allow")

	handler, _ := newRouter(testRouterConfig(t, nil))

	req := httptest.NewRequest(http.MethodGet, "/-/resync", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /-/resync = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "POST" {
		t.Errorf("Allow = %q, want POST", allow)
	}
}
