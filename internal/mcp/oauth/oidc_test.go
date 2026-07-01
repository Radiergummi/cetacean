package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOIDCDiscoveryServesASMetadata verifies that the OIDC Discovery 1.0
// well-known location serves the identical RFC 8414 AS metadata document,
// per the MCP 2025-11-25 authorization-server discovery enhancement.
func TestOIDCDiscoveryServesASMetadata(t *testing.T) {
	s := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")

	get := func(path string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("GET %s: expected application/json Content-Type, got %q", path, ct)
		}

		var doc map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
			t.Fatalf("GET %s: decode metadata: %v", path, err)
		}

		return doc
	}

	oidc := get("/.well-known/openid-configuration")
	rfc8414 := get("/.well-known/oauth-authorization-server")

	for _, key := range []string{"issuer", "authorization_endpoint", "token_endpoint"} {
		if oidc[key] != rfc8414[key] {
			t.Errorf("%s mismatch: oidc=%v rfc8414=%v", key, oidc[key], rfc8414[key])
		}
	}

	if oidc["issuer"] != "https://cetacean.test" {
		t.Errorf("issuer = %v", oidc["issuer"])
	}
}
