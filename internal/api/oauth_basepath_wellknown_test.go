package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
)

// RFC 9728 §3.1 builds the metadata URL by inserting the well-known segment
// after the authority, so under a base path a conformant client asks the
// authority root for a path that carries the prefix inside it. The base-path
// middleware used to refuse anything without the prefix, which made that
// derived URL unreachable through the real router however the routes were
// registered.
func TestDerivedDiscoveryURLResolvesUnderABasePath(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withBasePath("/cetacean"), withOAuthRoutes("/cetacean")},
		withCache(cache.New(nil)),
	)

	for _, path := range []string{
		// Derived per §3.1: authority root, prefix inside the path.
		"/.well-known/oauth-protected-resource/cetacean",
		"/.well-known/oauth-protected-resource/cetacean/mcp",
		// Beneath the deployment's prefix, which is what a proxy forwards.
		"/cetacean/.well-known/oauth-protected-resource",
		"/cetacean/.well-known/oauth-protected-resource/mcp",
	} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			// A path the mux never matches is answered by the SPA with 200 text/html,
			// so the content type is what distinguishes a document from a page.
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}
