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
	if allow := rec.Header().Get("Allow"); allow != "POST, OPTIONS" {
		t.Errorf("Allow = %q, want POST, OPTIONS", allow)
	}
}

// Outside /-/ too, a write method on a path some route serves is that path's
// to refuse, not the dashboard's: 405, with the methods the path does serve.
func TestWriteMethodOnARoutedPathIsNotAllowed(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/405-sends-allow")

	handler, _ := newRouter(testRouterConfig(t, nil))

	for _, tc := range []struct{ method, path, allow string }{
		{http.MethodPut, "/nodes", "GET, HEAD, OPTIONS"},
		{http.MethodDelete, "/services/svc1/scale", "PUT, OPTIONS"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", tc.method, tc.path, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != tc.allow {
			t.Errorf("%s %s: Allow = %q, want %q", tc.method, tc.path, got, tc.allow)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/no/such/path", nil))
	if rec.Code == http.StatusMethodNotAllowed {
		t.Errorf("POST to a path nothing routes = 405, want it left to the fallback")
	}
}

// A GET on a path only writes are routed at is the path's to refuse for a
// client that rules out HTML; one that takes it still gets the dashboard.
func TestReadOnAWriteOnlyPathIsNotAllowed(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/405-sends-allow")

	handler, _ := newRouter(testRouterConfig(t, nil))

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, accept := range []string{"application/json", "text/csv"} {
			req := httptest.NewRequest(method, "/services/svc1/scale", nil)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "PUT, OPTIONS" {
				t.Errorf("%s (Accept %q) = %d with Allow %q, want 405 with PUT, OPTIONS",
					method, accept, rec.Code, rec.Header().Get("Allow"))
			}
		}
	}

	for _, accept := range []string{"", "*/*", "text/html"} {
		req := httptest.NewRequest(http.MethodGet, "/services/svc1/scale", nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET with Accept %q = %d, want the dashboard", accept, rec.Code)
		}
	}
}

// OPTIONS says what a path serves, and for one that takes a PATCH, in which
// formats.
func TestOptionsReportsAllowAndAcceptPatch(t *testing.T) {
	spec.Satisfies(t, "http/rfc5789/accept-patch-is-advertised")

	handler, _ := newRouter(testRouterConfig(t, nil))

	for _, tc := range []struct{ path, allow, acceptPatch string }{
		{"/services/svc1/labels", "GET, HEAD, PATCH, OPTIONS", AcceptPatch},
		{"/services/svc1/resources", "GET, HEAD, PATCH, OPTIONS", AcceptMergePatch},
		{"/services/svc1/scale", "PUT, OPTIONS", ""},
		{"/nodes", "GET, HEAD, OPTIONS", ""},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, tc.path, nil))

		if rec.Code != http.StatusNoContent {
			t.Errorf("OPTIONS %s = %d, want 204", tc.path, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != tc.allow {
			t.Errorf("OPTIONS %s: Allow = %q, want %q", tc.path, got, tc.allow)
		}
		if got := rec.Header().Get("Accept-Patch"); got != tc.acceptPatch {
			t.Errorf("OPTIONS %s: Accept-Patch = %q, want %q", tc.path, got, tc.acceptPatch)
		}
	}
}
