package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp/oauth"
)

// withOAuthRoutes mounts the real authorization server the way main.go does:
// the server knows the base path for the URLs it publishes, and registers its
// routes under the empty prefix because basePathMiddleware strips first.
func withOAuthRoutes(basePath string) routerOption {
	srv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://swarm.example",
		BasePath:    basePath,
		MCPResource: "https://swarm.example" + basePath + "/mcp",
		MCP: config.MCPConfig{
			AccessTokenTTL:  time.Hour,
			RefreshTokenTTL: 720 * time.Hour,
			DCREnabled:      true,
			DCRRateLimit:    10,
			DCRMaxClients:   100,
		},
		SigningKey: []byte("cetacean-test-root-32-bytes-ok!!"),
	})

	return func(cfg *RouterConfig) {
		cfg.OAuthRoutes = srv.RegisterRoutes
	}
}

// TestAdvertisedJWKSURIServesAKeySet follows jwks_uri out of the metadata over
// the middleware stack a client actually meets. A route the mux never matches
// is answered by the SPA with 200 text/html, so a non-404 assertion would pass
// against an HTML page.
func TestAdvertisedJWKSURIServesAKeySet(t *testing.T) {
	for _, basePath := range []string{"", "/cetacean"} {
		t.Run("basePath="+basePath, func(t *testing.T) {
			router := newTestRouterWithConfig(
				t,
				[]routerOption{withBasePath(basePath), withOAuthRoutes(basePath)},
				withCache(cache.New(nil)),
			)

			metadata := httptest.NewRecorder()
			router.ServeHTTP(metadata, httptest.NewRequest(
				http.MethodGet,
				basePath+"/.well-known/oauth-authorization-server",
				nil,
			))

			if metadata.Code != http.StatusOK {
				t.Fatalf("metadata status = %d, want 200", metadata.Code)
			}

			var doc struct {
				JWKSURI string `json:"jwks_uri"`
			}
			if err := json.Unmarshal(metadata.Body.Bytes(), &doc); err != nil {
				t.Fatalf("unmarshal metadata: %v", err)
			}

			if doc.JWKSURI == "" {
				t.Fatal("metadata advertises no jwks_uri")
			}

			target, err := url.Parse(doc.JWKSURI)
			if err != nil {
				t.Fatalf("jwks_uri does not parse: %v", err)
			}

			// Errorf, not Fatalf: the follow-up request below still runs and
			// reports on the fetched document independently of this mismatch.
			if want := basePath + "/oauth/jwks"; target.Path != want {
				t.Errorf("jwks_uri path = %q, want %q", target.Path, want)
			}

			keys := httptest.NewRecorder()
			router.ServeHTTP(keys, httptest.NewRequest(http.MethodGet, target.Path, nil))

			if keys.Code != http.StatusOK {
				t.Fatalf("jwks status = %d, want 200", keys.Code)
			}

			if got := keys.Header().Get("Content-Type"); got != "application/jwk-set+json" {
				t.Fatalf("jwks Content-Type = %q, want application/jwk-set+json", got)
			}

			var set jose.JSONWebKeySet
			if err := json.Unmarshal(keys.Body.Bytes(), &set); err != nil {
				t.Fatalf("jwks body does not parse as a key set: %v", err)
			}

			if len(set.Keys) != 1 {
				t.Errorf("published keys = %d, want 1", len(set.Keys))
			}
		})
	}
}
