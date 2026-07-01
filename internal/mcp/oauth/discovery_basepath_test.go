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
	cfg := ServerConfig{
		Issuer:      "https://cetacean.test",
		BasePath:    "/cetacean",
		MCPResource: "https://cetacean.test/cetacean/mcp",
		MCP: config.MCPConfig{
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

	prmDoc := readJSONDoc(t, mux, "/cetacean/.well-known/oauth-protected-resource")
	servers, _ := prmDoc["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != wantBasePathIssuer {
		t.Errorf(
			"PRM authorization_servers = %v, want [%q]",
			prmDoc["authorization_servers"], wantBasePathIssuer,
		)
	}

	// The token's iss claim must match the advertised issuer, or a client that
	// validates iss against the discovered AS rejects the token.
	tok, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{Subject: "u"}, time.Hour)
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
