package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/config"
)

// newTestServer constructs a Server with in-memory stores, a known signing key,
// and AllowLoopback=true on the CIMD fetcher for test httptest servers.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := ServerConfig{
		Issuer:      "https://cetacean.test",
		BasePath:    "",
		MCPResource: "https://cetacean.test/mcp",
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
	s.cimd.AllowLoopback = true
	return s
}

// computeS256Challenge derives a PKCE code_challenge from a plain verifier.
func computeS256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// seedAuthCode pre-seeds an authorization code in the server's store and
// returns the raw code string.
func seedAuthCode(s *Server, data AuthCodeData) string {
	return s.authCodes.Issue(data, 60*time.Second)
}

// ---------------------------------------------------------------------------
// TestASMetadata
// ---------------------------------------------------------------------------

func TestASMetadata(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	s.HandleMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}

	var doc asMetadata
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	if doc.Issuer != "https://cetacean.test" {
		t.Errorf("issuer = %q", doc.Issuer)
	}
	if doc.AuthorizationEndpoint != "https://cetacean.test/oauth/authorize" {
		t.Errorf("authorization_endpoint = %q", doc.AuthorizationEndpoint)
	}
	if doc.TokenEndpoint != "https://cetacean.test/oauth/token" {
		t.Errorf("token_endpoint = %q", doc.TokenEndpoint)
	}
	if doc.RevocationEndpoint != "https://cetacean.test/oauth/revoke" {
		t.Errorf("revocation_endpoint = %q", doc.RevocationEndpoint)
	}
	if len(doc.CodeChallengeMethodsSupported) != 1 || doc.CodeChallengeMethodsSupported[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v", doc.CodeChallengeMethodsSupported)
	}
	if len(doc.ResponseTypesSupported) != 1 || doc.ResponseTypesSupported[0] != "code" {
		t.Errorf("response_types_supported = %v", doc.ResponseTypesSupported)
	}

	// DCR enabled: registration_endpoint must be present.
	if doc.RegistrationEndpoint == "" {
		t.Error("expected registration_endpoint when DCR enabled")
	}

	// DCR disabled: registration_endpoint must be absent.
	s2 := newTestServer(t)
	s2.cfg.MCP.DCREnabled = false
	s2.clients = nil
	rec2 := httptest.NewRecorder()
	s2.HandleMetadata(rec2, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))
	var doc2 asMetadata
	if err := json.NewDecoder(rec2.Body).Decode(&doc2); err != nil {
		t.Fatalf("decode doc2: %v", err)
	}
	if doc2.RegistrationEndpoint != "" {
		t.Errorf("expected no registration_endpoint when DCR disabled, got %q", doc2.RegistrationEndpoint)
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeWithPKCE - happy path
// ---------------------------------------------------------------------------

func TestTokenExchangeWithPKCE(t *testing.T) {
	s := newTestServer(t)

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := computeS256Challenge(verifier)

	code := seedAuthCode(s, AuthCodeData{
		ClientID:      "test-client",
		RedirectURI:   "http://localhost:8080/callback",
		CodeChallenge: challenge,
		Resource:      s.cfg.MCPResource,
		Subject:       "user@example.com",
		Groups:        []string{"admin"},
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {"test-client"},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected refresh_token")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q", resp.TokenType)
	}

	// Verify the JWT contains the expected audience.
	claims, err := s.tokenIssuer.VerifyAccessToken(resp.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if claims.Subject != "user@example.com" {
		t.Errorf("sub = %q", claims.Subject)
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeWrongVerifier
// ---------------------------------------------------------------------------

func TestTokenExchangeWrongVerifier(t *testing.T) {
	s := newTestServer(t)

	challenge := computeS256Challenge("correct-verifier")
	code := seedAuthCode(s, AuthCodeData{
		ClientID:      "test-client",
		RedirectURI:   "http://localhost/cb",
		CodeChallenge: challenge,
		Resource:      s.cfg.MCPResource,
		Subject:       "user",
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost/cb"},
		"client_id":     {"test-client"},
		"code_verifier": {"wrong-verifier"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var errResp oauthErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp.Error != "invalid_grant" {
		t.Errorf("error = %q, want invalid_grant", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeMismatchedResourceIndicator
// ---------------------------------------------------------------------------

func TestTokenExchangeMismatchedResourceIndicator(t *testing.T) {
	s := newTestServer(t)
	s.cfg.MCP.RequireResourceIndicator = false

	verifier := "test-verifier-for-resource"
	challenge := computeS256Challenge(verifier)
	code := seedAuthCode(s, AuthCodeData{
		ClientID:      "test-client",
		RedirectURI:   "http://localhost/cb",
		CodeChallenge: challenge,
		Resource:      s.cfg.MCPResource,
		Subject:       "user",
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost/cb"},
		"client_id":     {"test-client"},
		"code_verifier": {verifier},
		"resource":      {"https://other.server/mcp"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var errResp oauthErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp.Error != "invalid_target" {
		t.Errorf("error = %q, want invalid_target", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeRefreshHappy
// ---------------------------------------------------------------------------

func TestTokenExchangeRefreshHappy(t *testing.T) {
	s := newTestServer(t)

	// Seed a refresh token directly.
	refreshToken := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		Groups:   []string{"g1"},
		ClientID: "test-client",
		Resource: s.cfg.MCPResource,
	}, time.Hour)

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected new refresh_token")
	}
	if resp.RefreshToken == refreshToken {
		t.Error("new refresh_token must differ from old one")
	}

	// Old refresh token must no longer be valid.
	_, valid := s.refreshTokens.Validate(refreshToken)
	if valid {
		t.Error("old refresh token should be invalid after rotation")
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeRefreshTheft
// ---------------------------------------------------------------------------

func TestTokenExchangeRefreshTheft(t *testing.T) {
	s := newTestServer(t)

	refreshToken := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		ClientID: "test-client",
		Resource: s.cfg.MCPResource,
	}, time.Hour)

	// First rotation — consumes the original token.
	result := s.refreshTokens.Rotate(refreshToken, time.Hour)
	if !result.OK {
		t.Fatal("expected first rotation to succeed")
	}

	// Replay the already-consumed token — this is a theft signal.
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on theft, got %d", rec.Code)
	}
	var errResp oauthErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if errResp.Error != "invalid_grant" {
		t.Errorf("error = %q, want invalid_grant", errResp.Error)
	}

	// The entire grant family should now be revoked — new token from first rotation is dead.
	_, valid := s.refreshTokens.Validate(result.NewToken)
	if valid {
		t.Error("grant family should be revoked after theft detection")
	}
}

// ---------------------------------------------------------------------------
// TestRevocation
// ---------------------------------------------------------------------------

func TestRevocation(t *testing.T) {
	s := newTestServer(t)

	token := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		ClientID: "test-client",
		Resource: s.cfg.MCPResource,
	}, time.Hour)

	form := url.Values{"token": {token}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleRevoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Token should now be invalid.
	_, valid := s.refreshTokens.Validate(token)
	if valid {
		t.Error("token should be invalid after revocation")
	}
}

// ---------------------------------------------------------------------------
// TestRevocationUnknownToken
// ---------------------------------------------------------------------------

func TestRevocationUnknownToken(t *testing.T) {
	s := newTestServer(t)

	form := url.Values{"token": {"garbage-token-that-does-not-exist"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleRevoke(rec, req)

	// RFC 7009: always 200 even for unknown tokens.
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeRefreshMismatchedResource
// ---------------------------------------------------------------------------

func TestTokenExchangeRefreshMismatchedResource(t *testing.T) {
	srv := newTestServer(t)
	// Issue a refresh token bound to MCPResource.
	rt := srv.refreshTokens.Issue(RefreshTokenData{
		Subject:  "u@e",
		ClientID: "https://example.com/client",
		Resource: srv.cfg.MCPResource,
	}, time.Hour)

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {rt},
		"resource":      {"https://other-cetacean.example.com/mcp"}, // mismatch
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.HandleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for resource indicator mismatch", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_target") {
		t.Errorf("body must mention invalid_target: %s", rec.Body.String())
	}
	// Critical: the family should be revoked, so the original refresh token
	// is no longer valid.
	if _, ok := srv.refreshTokens.Validate(rt); ok {
		t.Errorf("refresh token must be revoked after a resource-mismatched rotate")
	}
}

// ---------------------------------------------------------------------------
// TestWriteUnauthorized
// ---------------------------------------------------------------------------

func TestWriteUnauthorized(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.WriteUnauthorized(rec, "invalid_token")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	wwwAuth := rec.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Fatal("expected WWW-Authenticate header")
	}
	if !strings.Contains(wwwAuth, `resource_metadata=`) {
		t.Errorf("WWW-Authenticate missing resource_metadata: %q", wwwAuth)
	}
	if !strings.Contains(wwwAuth, `error="invalid_token"`) {
		t.Errorf("WWW-Authenticate missing error: %q", wwwAuth)
	}
	// Must contain the PRM URL.
	expectedURL := s.cfg.Issuer + "/.well-known/oauth-protected-resource"
	if !strings.Contains(wwwAuth, expectedURL) {
		t.Errorf("WWW-Authenticate missing PRM URL %q: %q", expectedURL, wwwAuth)
	}
}

func TestHTTPQuotedString(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`plain`, `"plain"`},
		{`with "quotes"`, `"with \"quotes\""`},
		{`back\slash`, `"back\\slash"`},
		{"back`tick", "\"back`tick\""}, // backtick must NOT be Go-escaped
		{`https://例えば.test/x`, `"https://例えば.test/x"`},
	}
	for _, c := range cases {
		got := httpQuotedString(c.in)
		if got != c.want {
			t.Errorf("httpQuotedString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
