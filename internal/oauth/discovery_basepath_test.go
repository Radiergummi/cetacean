package oauth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/spec"
)

// wantBasePathIssuer is the external base URL a client discovers the AS at when
// Cetacean is mounted under a base path: Issuer + BasePath.
const wantBasePathIssuer = "https://cetacean.test/cetacean"

// TestDiscoveryIssuerIncludesBasePath reproduces E-5: when CETACEAN_BASE_PATH
// is non-empty, every advertised/claimed issuer identifier must be the URL the
// well-known documents are actually served from (Issuer + BasePath), so a
// client that derives the metadata location from the issuer resolves it
// instead of hitting a base-path-less 404.
func TestDiscoveryIssuerIncludesBasePath(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc8414/well-known-segment-precedes-the-path-component",
		"oauth/rfc8414/issuer-matches-the-retrieval-url",
		"oauth/rfc9728/well-known-segment-precedes-the-path-component",
	)

	cfg := ServerConfig{
		Issuer:   "https://cetacean.test",
		BasePath: "/cetacean",
		Resources: []Resource{
			{Path: "", Realm: "cetacean"},
			{Path: "/resource", Realm: "cetacean-resource"},
		},
		OAuth: config.OAuthConfig{
			AccessTokenTTL:           time.Hour,
			RefreshTokenTTL:          720 * time.Hour,
			RequireResourceIndicator: false,
			DCREnabled:               true,
			DCRRateLimit:             10,
			DCRMaxClients:            100,
		},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	}
	s := NewServer(cfg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "/cetacean")

	// A bare mux, so this test covers issuerID and nothing about routing: the
	// negotiation middleware, the base-path strip and the SPA fallback are all
	// absent. What a client actually receives is covered by
	// internal/api.TestAdvertisedJWKSURIServesAKeySet.

	asDoc := readJSONDoc(t, mux, "/cetacean/.well-known/oauth-authorization-server")
	if asDoc["issuer"] != wantBasePathIssuer {
		t.Errorf("AS metadata issuer = %v, want %q", asDoc["issuer"], wantBasePathIssuer)
	}

	// The metadata URL a client derives from the advertised issuer must equal
	// the URL the document is actually served at.
	if iss, ok := asDoc["issuer"].(string); ok {
		derived := iss + "/.well-known/oauth-authorization-server"
		served := "https://cetacean.test/cetacean/.well-known/oauth-authorization-server"
		if derived != served {
			t.Errorf("discovery URL from issuer = %q, but served at %q", derived, served)
		}
	}

	// One document per resource, each at the location RFC 9728 §3.1 derives from
	// its identifier's path — with the base path ahead of the well-known segment,
	// where this deployment is actually mounted. Both are asserted because the
	// root document describing the wrong resource is the confusion that makes a
	// token for one reach the other.
	// Each resource is reachable at both spellings: the location RFC 9728 §3.1
	// derives from the identifier — well-known after the authority, base path
	// inside it — and the one beneath this deployment's own prefix, which is what
	// a proxy forwarding only that prefix can deliver.
	for _, want := range []struct{ path, resource string }{
		{"/.well-known/oauth-protected-resource/cetacean", wantBasePathIssuer},
		{
			"/.well-known/oauth-protected-resource/cetacean/resource",
			wantBasePathIssuer + "/resource",
		},
		{"/cetacean/.well-known/oauth-protected-resource", wantBasePathIssuer},
		{
			"/cetacean/.well-known/oauth-protected-resource/resource",
			wantBasePathIssuer + "/resource",
		},
	} {
		prmDoc := readJSONDoc(t, mux, want.path)

		if prmDoc["resource"] != want.resource {
			t.Errorf("%s: resource = %v, want %q",
				want.path, prmDoc["resource"], want.resource)
		}

		servers, _ := prmDoc["authorization_servers"].([]any)
		if len(servers) != 1 || servers[0] != wantBasePathIssuer {
			t.Errorf("%s: authorization_servers = %v, want [%q]",
				want.path, prmDoc["authorization_servers"], wantBasePathIssuer)
		}
	}

	// The token's iss claim must match the advertised issuer, or a client that
	// validates iss against the discovered AS rejects the token.
	tok, err := s.tokenIssuer.IssueAccessToken(
		AccessTokenClaims{Subject: "u", ClientID: "c1"},
		s.resources.fallback,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if iss := jwtIssuerClaim(t, tok); iss != wantBasePathIssuer {
		t.Errorf("token iss = %q, want %q", iss, wantBasePathIssuer)
	}
}

func readJSONDoc(t *testing.T, h http.Handler, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, rec.Code)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	return doc
}

func jwtIssuerClaim(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("unmarshal JWT payload: %v", err)
	}

	iss, _ := m["iss"].(string)
	return iss
}
