package oauth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"

	"github.com/radiergummi/cetacean/internal/config"
)

func newJWKSTestServer(t *testing.T) *Server {
	t.Helper()

	return NewServer(ServerConfig{
		Issuer:      "https://swarm.example",
		MCPResource: "https://swarm.example/mcp",
		MCP: config.MCPConfig{
			AccessTokenTTL:  time.Hour,
			RefreshTokenTTL: 720 * time.Hour,
			DCREnabled:      true,
			DCRRateLimit:    10,
			DCRMaxClients:   100,
		},
		SigningKey: testRoot,
	})
}

func fetchJWKS(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, "")
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, jwksPath, nil))

	return rec
}

func TestJWKSDocumentShape(t *testing.T) {
	rec := fetchJWKS(t, newJWKSTestServer(t))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/jwk-set+json" {
		t.Errorf("Content-Type = %q, want application/jwk-set+json", got)
	}

	if got := rec.Header().Get("Cache-Control"); got != "max-age=3600" {
		t.Errorf("Cache-Control = %q, want max-age=3600", got)
	}

	// Assert the coordinates literally. go-jose both writes and reads them, so
	// parsing the document back with go-jose would not catch an encoding that
	// every other implementation rejects.
	var doc struct {
		Keys []struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(doc.Keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(doc.Keys))
	}

	key := doc.Keys[0]

	if key.Kty != "EC" || key.Crv != "P-256" || key.Use != "sig" || key.Alg != "ES256" {
		t.Errorf("key = %+v, want an EC P-256 sig/ES256 key", key)
	}

	for name, coord := range map[string]string{"x": key.X, "y": key.Y} {
		if len(coord) != 43 {
			t.Errorf(
				"%s is %d characters, want 43 (32 bytes, unpadded base64url)",
				name,
				len(coord),
			)
		}

		raw, err := base64.RawURLEncoding.DecodeString(coord)
		if err != nil {
			t.Errorf("%s does not decode as unpadded base64url: %v", name, err)

			continue
		}

		if len(raw) != 32 {
			t.Errorf("%s decodes to %d bytes, want 32", name, len(raw))
		}
	}

	km := mustDeriveKeys(t, testRoot)

	if key.Kid != km.kid {
		t.Errorf("kid = %q, want %q", key.Kid, km.kid)
	}

	// go-jose's JSONWebKey.MarshalJSON has a *ecdsa.PrivateKey branch that
	// emits "d"; nothing else in this test would notice if HandleJWKS passed
	// the private key instead of its public half.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	rawKeys, _ := raw["keys"].([]any)
	if len(rawKeys) != 1 {
		t.Fatalf("raw keys = %d, want 1", len(rawKeys))
	}

	rawKey, _ := rawKeys[0].(map[string]any)
	for _, private := range []string{"d", "p", "q", "dp", "dq", "qi", "k"} {
		if _, ok := rawKey[private]; ok {
			t.Errorf("published key carries private member %q", private)
		}
	}
}

// TestPublishedKeyVerifiesAToken is the load-bearing check: an implementation
// that is not ours verifies a token we minted, using only what we published.
func TestPublishedKeyVerifiesAToken(t *testing.T) {
	s := newJWKSTestServer(t)

	token, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "alice",
		ClientID: "https://client.example/id.json",
	}, time.Hour)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	var set jose.JSONWebKeySet
	if err := json.Unmarshal(fetchJWKS(t, s).Body.Bytes(), &set); err != nil {
		t.Fatalf("unmarshal key set: %v", err)
	}

	signature, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("go-jose could not parse the token: %v", err)
	}

	matching := set.Key(signature.Signatures[0].Header.KeyID)
	if len(matching) != 1 {
		t.Fatalf("published set has %d keys for the token's kid, want 1", len(matching))
	}

	if _, err := signature.Verify(matching[0].Key); err != nil {
		t.Fatalf("go-jose rejected a token we minted: %v", err)
	}
}

func TestMetadataAdvertisesTheKeySet(t *testing.T) {
	s := newJWKSTestServer(t)

	rec := httptest.NewRecorder()
	s.HandleMetadata(
		rec,
		httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil),
	)

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := doc["jwks_uri"]; got != "https://swarm.example/oauth/jwks" {
		t.Errorf("jwks_uri = %v, want https://swarm.example/oauth/jwks", got)
	}
}

func TestMetadataOmitsTheKeySetWithoutAKey(t *testing.T) {
	s := NewServer(ServerConfig{
		Issuer:      "https://swarm.example",
		MCPResource: "https://swarm.example/mcp",
		MCP:         config.MCPConfig{AccessTokenTTL: time.Hour},
	})

	rec := httptest.NewRecorder()
	s.HandleMetadata(
		rec,
		httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil),
	)

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := doc["jwks_uri"]; ok {
		t.Error("jwks_uri is advertised with no key to serve at it")
	}
}
