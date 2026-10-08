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
	"github.com/radiergummi/cetacean/internal/spec"
)

// newTestServer constructs a Server with in-memory stores, a known signing key,
// and AllowLoopback=true on the CIMD fetcher for test httptest servers.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := ServerConfig{
		Issuer:    "https://cetacean.test",
		BasePath:  "",
		Resources: []Resource{{Path: "/resource", Realm: "cetacean", Name: "Cetacean Resource"}},
		OAuth: config.OAuthConfig{
			AccessTokenTTL:           time.Hour,
			RefreshTokenTTL:          720 * time.Hour,
			ConsentTTL:               testConsentTTL,
			RequireResourceIndicator: false,
			DCREnabled:               true,
			DCRRateLimit:             10,
			DCRMaxClients:            100,
			CIMDEnabled:              true,
		},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	}
	s := NewServer(cfg)
	s.cimd.AllowLoopback = true
	return s
}

// newPersistingServer is newTestServer wired to a state file at path, for tests
// that restart a server over the same durable state. The resource path stays
// explicit: these tests pin it to the audience their fixture tokens were written
// for.
func newPersistingServer(t *testing.T, path, resourcePath string) *Server {
	t.Helper()

	s := NewServer(ServerConfig{
		Issuer:    "https://cetacean.test",
		Resources: []Resource{{Path: resourcePath, Realm: "cetacean"}},
		OAuth: config.OAuthConfig{
			AccessTokenTTL:  time.Hour,
			RefreshTokenTTL: 720 * time.Hour,
			ConsentTTL:      testConsentTTL,
		},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
		StatePath:  path,
	})
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
	spec.Satisfies(t,
		"oauth/rfc9700/as-metadata-published",
		"oauth/rfc9700/pkce-support-detectable",
		"oauth/rfc9700/pkce-support-advertised-in-metadata",
		"oauth/rfc8414/issuer-required",
		"oauth/rfc8414/authorization-endpoint-required",
		"oauth/rfc8414/token-endpoint-required",
		"oauth/rfc8414/response-types-supported-required",
		"oauth/rfc8414/response-is-200-json",
		"oauth/rfc8252/client-authentication-not-required",
		"oauth/oauth-2-1/challenge-and-verifier-parameters-supported",
	)

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
	if len(doc.CodeChallengeMethodsSupported) != 1 ||
		doc.CodeChallengeMethodsSupported[0] != "S256" {
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
	s2.cfg.OAuth.DCREnabled = false
	s2.clients = nil
	rec2 := httptest.NewRecorder()
	s2.HandleMetadata(
		rec2,
		httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil),
	)
	var doc2 asMetadata
	if err := json.NewDecoder(rec2.Body).Decode(&doc2); err != nil {
		t.Fatalf("decode doc2: %v", err)
	}
	if doc2.RegistrationEndpoint != "" {
		t.Errorf(
			"expected no registration_endpoint when DCR disabled, got %q",
			doc2.RegistrationEndpoint,
		)
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeWithPKCE - happy path
// ---------------------------------------------------------------------------

func TestTokenExchangeWithPKCE(t *testing.T) {
	spec.Satisfies(t,
		"oauth/oauth-2-1/access-token-required-in-the-response",
		"oauth/oauth-2-1/token-type-required-in-the-response",
		"oauth/oauth-2-1/expires-in-recommended",
		"oauth/oauth-2-1/refresh-token-optional-in-the-response",
		"oauth/rfc7636/a-matching-verifier-continues-processing",
	)

	s := newTestServer(t)

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := computeS256Challenge(verifier)

	code := seedAuthCode(s, AuthCodeData{
		ClientID:      "test-client",
		RedirectURI:   "http://localhost:8080/callback",
		CodeChallenge: challenge,
		Resource:      s.resources.fallback,
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
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in = %d, want the token's lifetime in seconds", resp.ExpiresIn)
	}

	// Verify the JWT contains the expected audience.
	claims, err := s.tokenIssuer.VerifyAccessToken(resp.AccessToken, s.resources.fallback)
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

// Both verifiers are RFC 7636 shaped, so the refusal can only come from the
// challenge comparison. A short one is refused by validateCodeVerifier first,
// with the same invalid_grant, and never reaches the comparison at all.
func TestTokenExchangeWrongVerifier(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7636/verifier-must-match-challenge",
		"oauth/rfc9700/code-challenge-bound-to-the-code",
		"oauth/oauth-2-1/error-parameter-required",
		"oauth/oauth-2-1/error-description-optional",
	)

	s := newTestServer(t)

	const (
		correct = "correct-verifier-correct-verifier-correct-ver"
		wrong   = "wrong-verifier-wrong-verifier-wrong-verifier-"
	)

	challenge := computeS256Challenge(correct)
	code := seedAuthCode(s, AuthCodeData{
		ClientID:      "test-client",
		RedirectURI:   "http://localhost/cb",
		CodeChallenge: challenge,
		Resource:      s.resources.fallback,
		Subject:       "user",
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost/cb"},
		"client_id":     {"test-client"},
		"code_verifier": {wrong},
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
	if errResp.ErrorDescription == "" {
		t.Error("error_description is absent; the refusal says only that it happened")
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeMismatchedResourceIndicator
// ---------------------------------------------------------------------------

func TestTokenExchangeMismatchedResourceIndicator(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc8707/unknown-resource-refused",
		"oauth/rfc9700/resource-parameter-may-select-the-server",
	)

	s := newTestServer(t)
	s.cfg.OAuth.RequireResourceIndicator = false

	verifier := "test-verifier-for-resource"
	challenge := computeS256Challenge(verifier)
	code := seedAuthCode(s, AuthCodeData{
		ClientID:      "test-client",
		RedirectURI:   "http://localhost/cb",
		CodeChallenge: challenge,
		Resource:      s.resources.fallback,
		Subject:       "user",
	})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost/cb"},
		"client_id":     {"test-client"},
		"code_verifier": {verifier},
		"resource":      {"https://other.server/resource"},
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
		Resource: s.resources.fallback,
	}, time.Hour)

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {"test-client"},
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

// RFC 6749 §6 requires client_id of a client that does not authenticate, and
// every client here is one. An absent parameter is a malformed request, not a
// grant that failed to match.
func TestTokenExchangeRefreshWithoutClientID(t *testing.T) {
	s := newTestServer(t)

	refreshToken := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		ClientID: "test-client",
		Resource: s.resources.fallback,
	}, time.Hour)

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	s.HandleToken(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_request") {
		t.Errorf("error = %s, want invalid_request", w.Body.String())
	}

	// The grant must survive: a malformed request is the client's bug, and
	// burning the family would make it the user's.
	if _, ok := s.refreshTokens.Validate(refreshToken); !ok {
		t.Error("a request missing client_id consumed the refresh token")
	}
}

func TestTokenExchangeRefreshTheft(t *testing.T) {
	s := newTestServer(t)

	refreshToken := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		ClientID: "test-client",
		Resource: s.resources.fallback,
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
		"client_id":     {"test-client"},
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
	spec.Satisfies(t, "oauth/rfc7009/refresh-token-revocation-supported")

	s := newTestServer(t)

	token := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		ClientID: "test-client",
		Resource: s.resources.fallback,
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

// The hint is advisory: this server looks a token up the one way it can, so a
// client that guesses the type wrong still gets its token revoked.
func TestRevocationIgnoresAWrongTokenTypeHint(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7009/hint-failure-searches-every-token-type")

	s := newTestServer(t)

	token := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		ClientID: "test-client",
		Resource: s.resources.fallback,
	}, time.Hour)

	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleRevoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if _, valid := s.refreshTokens.Validate(token); valid {
		t.Error("a wrong token_type_hint left the refresh token usable")
	}
}

// Revocation reaches the grant, not the tokens already minted from it: an
// access token is a self-contained JWT this server never sees again.
func TestRevocationDoesNotReachAnAccessToken(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc7009/access-token-revocation-supported",
		"oauth/rfc7009/cascade-to-access-tokens",
	)

	s := newTestServer(t)

	// The server's own issuer, or revocation could not reach the token even in
	// principle and this proves nothing about what revocation does.
	access, err := s.tokenIssuer.IssueAccessToken(
		AccessTokenClaims{Subject: "user", ClientID: "test-client"},
		s.resources.fallback,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	refresh := s.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user",
		ClientID: "test-client",
		Resource: s.resources.fallback,
	}, time.Hour)

	for _, token := range []string{refresh, access} {
		form := url.Values{"token": {token}}
		req := httptest.NewRequest(
			http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.HandleRevoke(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	}

	// The grant is gone, so no further token can be refreshed out of it.
	if _, valid := s.refreshTokens.Validate(refresh); valid {
		t.Error("the refresh token survived revocation")
	}

	// The access token is not, and keeps working until it expires.
	if _, err := s.tokenIssuer.VerifyAccessToken(access, s.resources.fallback); err != nil {
		t.Errorf("revocation reached an access token after all: %v", err)
	}
}

// A request naming no token is answered 200 rather than the invalid_request
// §2.2.1 provides for, so a client that misspells the parameter is told its
// token is gone when nothing was looked at.
func TestRevocationWithoutATokenIsAccepted(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7009/token-parameter-required")

	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleRevoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the 200 this server currently answers", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// TestCodeVerifier_RFC7636Length covers M-18: PKCE verifier length and
// alphabet enforcement against RFC 7636 §4.1.
func TestCodeVerifier_RFC7636Length(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7636/verifier-character-set")

	cases := []struct {
		name     string
		verifier string
		ok       bool
	}{
		{"too short (42 chars)", strings.Repeat("a", 42), false},
		{"at minimum (43)", strings.Repeat("a", 43), true},
		{"at maximum (128)", strings.Repeat("a", 128), true},
		{"too long (129)", strings.Repeat("a", 129), false},
		{"empty", "", false},
		{"illegal character", strings.Repeat("a", 42) + "!", false},
		{"all unreserved", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmno-._~0123", true},
		{"range ends", "AZaz09" + strings.Repeat("a", 37), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateCodeVerifier(c.verifier)
			if (err == nil) != c.ok {
				t.Errorf("ok=%v, got err=%v", c.ok, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTokenExchangeRefreshMismatchedResource
// ---------------------------------------------------------------------------

func TestTokenExchangeRefreshMismatchedResource(t *testing.T) {
	srv := newTestServer(t)
	// Issue a refresh token bound to the configured resource.
	rt := srv.refreshTokens.Issue(RefreshTokenData{
		Subject:  "u@e",
		ClientID: "https://example.com/client",
		Resource: srv.resources.fallback,
	}, time.Hour)

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {rt},
		"resource":      {"https://other-cetacean.example.com/resource"}, // mismatch
		"client_id":     {"https://example.com/client"},
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
	// Per M-20: a resource-indicator typo must NOT burn the grant family —
	// the bound resource validation runs before consuming the refresh token,
	// so the original token must still be live.
	if _, ok := srv.refreshTokens.Validate(rt); !ok {
		t.Errorf("refresh token must remain valid after a non-rotating mismatch")
	}
}

// ---------------------------------------------------------------------------
// TestUnauthorizedHeader
// ---------------------------------------------------------------------------

func TestUnauthorizedHeader(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc6750/challenge-uses-the-bearer-scheme",
		"oauth/rfc6750/challenge-carries-an-auth-param",
		"oauth/rfc6750/error-attribute-on-a-failed-token",
	)

	s := newTestServer(t)

	got := s.UnauthorizedHeader(s.resources.fallback, "invalid_token")

	// Asserted whole rather than by substring: the realm and the parameter order
	// are what a client parses, and a piecewise check cannot see either change.
	// The metadata URL carries the resource's own path, so it names this
	// resource rather than whichever one sits at the root.
	want := `Bearer realm="cetacean", ` +
		`resource_metadata="https://cetacean.test/.well-known/oauth-protected-resource` +
		testResourcePath + `", ` +
		`error="invalid_token"`

	if got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestHTTPQuotedString(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/a-quoted-pair-only-escapes-what-it-must")

	cases := []struct {
		in, want string
	}{
		{`plain`, `"plain"`},
		{`with "quotes"`, `"with \"quotes\""`},
		{`back\slash`, `"back\\slash"`},
		{"back`tick", "\"back`tick\""}, // backtick must NOT be Go-escaped
		{`https://例えば.test/x`, `"https://例えば.test/x"`},
		// qdtext admits HTAB and SP but no other control, and no DEL. None can
		// be escaped into range either: quoted-pair takes only HTAB, SP, VCHAR
		// and obs-text.
		{"tab\tand space", "\"tab\tand space\""},
		{"split\r\nheader", `"splitheader"`},
		{"nul\x00and\x7fdel", `"nulanddel"`},
		// The ends of VCHAR, and the first byte of obs-text.
		{"!~\x80", "\"!~\x80\""},
	}
	for _, c := range cases {
		got := httpQuotedString(c.in)
		if got != c.want {
			t.Errorf("httpQuotedString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// RFC 7636 Appendix B's worked example. Ours is the only S256 implementation
// in the flow, so a vector from the RFC is what says it computes the same
// challenge a client does rather than merely agreeing with itself.
func TestS256MatchesTheRFC7636Vector(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7636/verifier-character-set")

	const (
		verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	)

	if err := validateCodeVerifier(verifier); err != nil {
		t.Fatalf("the RFC's own verifier is refused as malformed: %v", err)
	}

	if got := computeS256Challenge(verifier); got != challenge {
		t.Errorf("challenge = %q, want the RFC's %q", got, challenge)
	}

	if !verifySHA256Challenge(verifier, challenge) {
		t.Error("the RFC's verifier and challenge do not verify against each other")
	}
}

// The empty verifier's challenge is computable by anyone, so an empty verifier
// is refused before any comparison could accept it.
func TestAnEmptyVerifierNeverMatches(t *testing.T) {
	if verifySHA256Challenge("", computeS256Challenge("")) {
		t.Error("an empty verifier matched the challenge computed from it")
	}
}
