package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/go-jose/go-jose/v4"
)

// mockIDPServer is a configurable mock OIDC identity provider that serves
// discovery, JWKS, and token exchange endpoints with real cryptographic
// signatures so that go-oidc verification passes end-to-end.
type mockIDPServer struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	keyID    string
	clientID string

	// tokenHandler can be overridden per-test to customize the token response.
	tokenHandler http.HandlerFunc
}

func newMockIDP(t *testing.T, clientID string) *mockIDPServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	idp := &mockIDPServer{
		key:      key,
		keyID:    "test-key-1",
		clientID: clientID,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q
		}`, idp.server.URL, idp.server.URL+"/authorize", idp.server.URL+"/token", idp.server.URL+"/jwks")
	})

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwk := jose.JSONWebKey{
			Key:       &key.PublicKey,
			KeyID:     idp.keyID,
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if idp.tokenHandler != nil {
			idp.tokenHandler(w, r)
			return
		}
		http.Error(w, "token handler not configured", http.StatusInternalServerError)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)

	return idp
}

// issueIDToken creates a signed JWT with the given claims.
func (idp *mockIDPServer) issueIDToken(t *testing.T, nonce string, expiry time.Time) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), idp.keyID).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	now := time.Now()
	claims := josejwt.Claims{
		Issuer:    idp.server.URL,
		Subject:   "user-42",
		Audience:  josejwt.Audience{idp.clientID},
		IssuedAt:  josejwt.NewNumericDate(now),
		Expiry:    josejwt.NewNumericDate(expiry),
	}

	extra := map[string]any{
		"name":  "Test User",
		"email": "test@example.com",
		"nonce": nonce,
	}

	raw, err := josejwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	return raw
}

// issueIDTokenMultiAud creates a signed JWT with multiple audiences and an
// optional azp claim for testing OIDC Core Section 3.1.3.7 compliance.
func (idp *mockIDPServer) issueIDTokenMultiAud(t *testing.T, nonce string, expiry time.Time, audiences []string, azp string) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), idp.keyID).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	now := time.Now()
	claims := josejwt.Claims{
		Issuer:   idp.server.URL,
		Subject:  "user-42",
		Audience: josejwt.Audience(audiences),
		IssuedAt: josejwt.NewNumericDate(now),
		Expiry:   josejwt.NewNumericDate(expiry),
	}

	extra := map[string]any{
		"name":  "Test User",
		"email": "test@example.com",
		"nonce": nonce,
	}
	if azp != "" {
		extra["azp"] = azp
	}

	raw, err := josejwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	return raw
}

// setTokenHandler configures the mock to return a successful token response
// using the given ID token and access token expiry.
func (idp *mockIDPServer) setTokenHandler(t *testing.T, idToken string, accessExpiry time.Time) {
	t.Helper()
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"expires_in":   int(time.Until(accessExpiry).Seconds()),
			"id_token":     idToken,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// newProviderWithIDP creates an OIDCProvider pointing at the mock IDP.
func newProviderWithIDP(t *testing.T, idp *mockIDPServer, redirectURL string) *OIDCProvider {
	t.Helper()

	p, err := NewOIDCProvider(t.Context(), OIDCProviderConfig{
		Issuer:       idp.server.URL,
		ClientID:     idp.clientID,
		ClientSecret: "test-secret",
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "profile", "email"},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	return p
}

// initiateLogin performs a login redirect and returns the flow cookies + the
// nonce and state values embedded in them.
func initiateLogin(t *testing.T, p *OIDCProvider) (cookies []*http.Cookie, state, nonce, verifier string) {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/auth/login?redirect=/dashboard", nil)
	w := httptest.NewRecorder()
	p.handleLogin(w, r)

	cookies = w.Result().Cookies()

	for _, c := range cookies {
		switch c.Name {
		case "cetacean_auth_state":
			state = c.Value
		case "cetacean_auth_nonce":
			nonce = c.Value
		case "cetacean_auth_verifier":
			verifier = c.Value
		}
	}

	if state == "" || nonce == "" || verifier == "" {
		t.Fatal("login did not set all required cookies")
	}

	return cookies, state, nonce, verifier
}

// buildCallbackRequest constructs a callback request with the given query
// params and attaches the provided cookies.
func buildCallbackRequest(cookies []*http.Cookie, query url.Values) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?"+query.Encode(), nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

// --- End-to-end callback tests ---

func TestCallback_HappyPath(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, nonce, _ := initiateLogin(t, p)

	idToken := idp.issueIDToken(t, nonce, time.Now().Add(time.Hour))
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	query := url.Values{
		"code":  {"auth-code-123"},
		"state": {state},
	}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()

	p.handleCallback(w, r)
	resp := w.Result()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	// Should redirect to the original redirect URL from login.
	location := resp.Header.Get("Location")
	if location != "/dashboard" {
		t.Errorf("redirect location = %q, want %q", location, "/dashboard")
	}

	// Should have set a session cookie.
	var hasSession bool
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && c.MaxAge > 0 {
			hasSession = true
		}
	}
	if !hasSession {
		t.Error("missing session cookie after successful callback")
	}

	// Auth flow cookies should be cleared.
	for _, c := range resp.Cookies() {
		switch c.Name {
		case "cetacean_auth_state", "cetacean_auth_nonce", "cetacean_auth_verifier", "cetacean_auth_redirect":
			if c.MaxAge != -1 {
				t.Errorf("flow cookie %q not cleared (MaxAge=%d)", c.Name, c.MaxAge)
			}
		}
	}

	// Verify the session contains the right identity.
	sessionCookie := findCookie(resp.Cookies(), cookieName)
	if sessionCookie == nil {
		t.Fatal("session cookie not found")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookie)
	id, err := p.session.Get(req)
	if err != nil {
		t.Fatalf("session.Get: %v", err)
	}
	if id.Subject != "user-42" {
		t.Errorf("Subject = %q, want %q", id.Subject, "user-42")
	}
	if id.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "test@example.com")
	}
	if id.DisplayName != "Test User" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Test User")
	}
}

func TestCallback_HappyPath_DefaultRedirect(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	// Initiate login without a redirect parameter.
	r := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()
	p.handleLogin(w, r)

	cookies := w.Result().Cookies()
	var state, nonce string
	for _, c := range cookies {
		switch c.Name {
		case "cetacean_auth_state":
			state = c.Value
		case "cetacean_auth_nonce":
			nonce = c.Value
		}
	}

	idToken := idp.issueIDToken(t, nonce, time.Now().Add(time.Hour))
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	cr := buildCallbackRequest(cookies, query)
	cw := httptest.NewRecorder()
	p.handleCallback(cw, cr)

	if loc := cw.Result().Header.Get("Location"); loc != "/" {
		t.Errorf("default redirect = %q, want %q", loc, "/")
	}
}

func TestCallback_SessionTTL_CappedAtMax(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, nonce, _ := initiateLogin(t, p)

	idToken := idp.issueIDToken(t, nonce, time.Now().Add(24*time.Hour))
	// Access token expires in 24h — session should be capped at maxSessionTTL (8h).
	idp.setTokenHandler(t, idToken, time.Now().Add(24*time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	sessionCookie := findCookie(w.Result().Cookies(), cookieName)
	if sessionCookie == nil {
		t.Fatal("missing session cookie")
	}
	// MaxAge should be at most maxSessionTTL.
	if sessionCookie.MaxAge > int(maxSessionTTL.Seconds()) {
		t.Errorf("session MaxAge = %d, want <= %d", sessionCookie.MaxAge, int(maxSessionTTL.Seconds()))
	}
}

func TestCallback_SessionTTL_UsesIDTokenExpiry(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, nonce, _ := initiateLogin(t, p)

	// ID token expires in 30 minutes — shorter than maxSessionTTL.
	// Access token has a longer expiry, but session TTL should follow the
	// ID token (authentication validity), not the access token.
	idToken := idp.issueIDToken(t, nonce, time.Now().Add(30*time.Minute))
	idp.setTokenHandler(t, idToken, time.Now().Add(24*time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	sessionCookie := findCookie(w.Result().Cookies(), cookieName)
	if sessionCookie == nil {
		t.Fatal("missing session cookie")
	}
	// Allow 5s tolerance for test execution time.
	if sessionCookie.MaxAge > int((30*time.Minute + 5*time.Second).Seconds()) {
		t.Errorf("session MaxAge = %d, expected ~%d", sessionCookie.MaxAge, int((30 * time.Minute).Seconds()))
	}
}

func TestCallback_RFC9207_IssuerValidation(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, nonce, _ := initiateLogin(t, p)

	idToken := idp.issueIDToken(t, nonce, time.Now().Add(time.Hour))
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	// Callback with correct iss parameter should succeed.
	query := url.Values{
		"code":  {"code"},
		"state": {state},
		"iss":   {idp.server.URL},
	}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusFound {
		t.Errorf("valid iss: status = %d, want %d", w.Result().StatusCode, http.StatusFound)
	}
}

func TestCallback_RFC9207_IssuerMismatch(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, _, _ := initiateLogin(t, p)

	query := url.Values{
		"code":  {"code"},
		"state": {state},
		"iss":   {"https://evil-idp.example.com"},
	}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("mismatched iss: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

// --- Callback error path tests ---

func TestCallback_IdPError(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, _, _, _ := initiateLogin(t, p)

	query := url.Values{
		"error":             {"access_denied"},
		"error_description": {"user denied consent"},
	}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Errorf("IdP error: status = %d, want %d", w.Result().StatusCode, http.StatusForbidden)
	}
}

func TestCallback_StateMismatch(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, _, _, _ := initiateLogin(t, p)

	query := url.Values{
		"code":  {"code"},
		"state": {"wrong-state-value"},
	}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("state mismatch: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestCallback_MissingStateCookie(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	// No cookies at all.
	query := url.Values{
		"code":  {"code"},
		"state": {"some-state"},
	}
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?"+query.Encode(), nil)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("missing state cookie: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestCallback_MissingNonceCookie(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, _, _ := initiateLogin(t, p)

	// Remove the nonce cookie.
	var filtered []*http.Cookie
	for _, c := range cookies {
		if c.Name != "cetacean_auth_nonce" {
			filtered = append(filtered, c)
		}
	}

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(filtered, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("missing nonce cookie: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestCallback_MissingVerifierCookie(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, _, _ := initiateLogin(t, p)

	// Remove the verifier cookie.
	var filtered []*http.Cookie
	for _, c := range cookies {
		if c.Name != "cetacean_auth_verifier" {
			filtered = append(filtered, c)
		}
	}

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(filtered, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("missing verifier cookie: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestCallback_NonceMismatch(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, _, _ := initiateLogin(t, p)

	// Issue a token with a different nonce than what was stored in the cookie.
	idToken := idp.issueIDToken(t, "wrong-nonce", time.Now().Add(time.Hour))
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("nonce mismatch: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestCallback_TokenExchangeFailure(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, _, _ := initiateLogin(t, p)

	// Token endpoint returns an error.
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"code expired"}`)
	}

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("token exchange failure: status = %d, want %d", w.Result().StatusCode, http.StatusInternalServerError)
	}
}

func TestCallback_MissingIDToken(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, _, _ := initiateLogin(t, p)

	// Token endpoint returns a valid response but without id_token.
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"at","token_type":"Bearer","expires_in":3600}`)
	}

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("missing id_token: status = %d, want %d", w.Result().StatusCode, http.StatusInternalServerError)
	}
}

func TestCallback_InvalidIDTokenSignature(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, nonce, _ := initiateLogin(t, p)

	// Create a second IDP with a different key, use it to sign the token.
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: otherKey},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "wrong-key").WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	now := time.Now()
	claims := josejwt.Claims{
		Issuer:   idp.server.URL,
		Subject:  "user-42",
		Audience: josejwt.Audience{idp.clientID},
		IssuedAt: josejwt.NewNumericDate(now),
		Expiry:   josejwt.NewNumericDate(now.Add(time.Hour)),
	}
	extra := map[string]any{"nonce": nonce}
	badToken, err := josejwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatalf("sign bad token: %v", err)
	}

	idp.setTokenHandler(t, badToken, now.Add(time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("invalid signature: status = %d, want %d", w.Result().StatusCode, http.StatusInternalServerError)
	}
}

func TestCallback_ExpiredIDToken(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, nonce, _ := initiateLogin(t, p)

	// Issue an already-expired ID token.
	idToken := idp.issueIDToken(t, nonce, time.Now().Add(-time.Hour))
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("expired id_token: status = %d, want %d", w.Result().StatusCode, http.StatusInternalServerError)
	}
}

func TestCallback_MissingCode(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, _, _ := initiateLogin(t, p)

	// Token endpoint will fail because no code is sent.
	idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_request","error_description":"missing code"}`)
	}

	// No "code" parameter.
	query := url.Values{"state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	// The code reaches the Exchange call with an empty code, which the token
	// endpoint rejects, resulting in a 500 from handleCallback.
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Errorf("missing code: status = %d, want %d", w.Result().StatusCode, http.StatusInternalServerError)
	}
}

// --- Cookie clearing on error paths ---

func TestCallback_ClearsCookiesOnAllErrors(t *testing.T) {
	flowCookieNames := []string{
		"cetacean_auth_state",
		"cetacean_auth_nonce",
		"cetacean_auth_verifier",
		"cetacean_auth_redirect",
	}

	assertFlowCookiesCleared := func(t *testing.T, resp *http.Response) {
		t.Helper()
		cleared := map[string]bool{}
		for _, c := range resp.Cookies() {
			if c.MaxAge == -1 {
				cleared[c.Name] = true
			}
		}
		for _, name := range flowCookieNames {
			if !cleared[name] {
				t.Errorf("cookie %s not cleared on error path", name)
			}
		}
	}

	t.Run("StateMismatch", func(t *testing.T) {
		idp := newMockIDP(t, "test-client")
		p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")
		cookies, _, _, _ := initiateLogin(t, p)

		query := url.Values{"code": {"code"}, "state": {"wrong"}}
		r := buildCallbackRequest(cookies, query)
		w := httptest.NewRecorder()
		p.handleCallback(w, r)

		assertFlowCookiesCleared(t, w.Result())
	})

	t.Run("IdPError", func(t *testing.T) {
		idp := newMockIDP(t, "test-client")
		p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")
		cookies, _, _, _ := initiateLogin(t, p)

		query := url.Values{"error": {"access_denied"}}
		r := buildCallbackRequest(cookies, query)
		w := httptest.NewRecorder()
		p.handleCallback(w, r)

		assertFlowCookiesCleared(t, w.Result())
	})

	t.Run("TokenExchangeFailure", func(t *testing.T) {
		idp := newMockIDP(t, "test-client")
		p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")
		cookies, state, _, _ := initiateLogin(t, p)

		idp.tokenHandler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
		}

		query := url.Values{"code": {"code"}, "state": {state}}
		r := buildCallbackRequest(cookies, query)
		w := httptest.NewRecorder()
		p.handleCallback(w, r)

		assertFlowCookiesCleared(t, w.Result())
	})

	t.Run("NonceMismatch", func(t *testing.T) {
		idp := newMockIDP(t, "test-client")
		p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")
		cookies, state, _, _ := initiateLogin(t, p)

		idToken := idp.issueIDToken(t, "wrong-nonce", time.Now().Add(time.Hour))
		idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

		query := url.Values{"code": {"code"}, "state": {state}}
		r := buildCallbackRequest(cookies, query)
		w := httptest.NewRecorder()
		p.handleCallback(w, r)

		assertFlowCookiesCleared(t, w.Result())
	})
}

// --- AZP validation in callback flow (OIDC Core 3.1.3.7) ---

func TestCallback_AzpValidation_MultiAudience_Valid(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")
	cookies, state, nonce, _ := initiateLogin(t, p)

	// Issue a multi-audience token with correct azp.
	idToken := idp.issueIDTokenMultiAud(t, nonce, time.Now().Add(time.Hour),
		[]string{"test-client", "other-service"}, "test-client")
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusFound {
		t.Errorf("valid azp: status = %d, want %d", w.Result().StatusCode, http.StatusFound)
	}
}

func TestCallback_AzpValidation_MultiAudience_MissingAzp(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")
	cookies, state, nonce, _ := initiateLogin(t, p)

	// Issue a multi-audience token WITHOUT azp.
	idToken := idp.issueIDTokenMultiAud(t, nonce, time.Now().Add(time.Hour),
		[]string{"test-client", "other-service"}, "")
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("missing azp: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestCallback_AzpValidation_MultiAudience_WrongAzp(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")
	cookies, state, nonce, _ := initiateLogin(t, p)

	// Issue a multi-audience token with WRONG azp.
	idToken := idp.issueIDTokenMultiAud(t, nonce, time.Now().Add(time.Hour),
		[]string{"test-client", "other-service"}, "other-service")
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(cookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Errorf("wrong azp: status = %d, want %d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

// --- Whoami tests ---

func TestWhoami_ValidSession(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	// Create a session cookie.
	rec := httptest.NewRecorder()
	p.session.Set(rec, &Identity{
		Subject:     "user-1",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Provider:    "oidc",
	}, maxSessionTTL)

	sessionCookie := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	r.AddCookie(sessionCookie)
	w := httptest.NewRecorder()

	p.handleWhoami(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var id Identity
	if err := json.NewDecoder(w.Body).Decode(&id); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if id.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", id.Subject, "user-1")
	}
}

func TestWhoami_NoSession(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	r := httptest.NewRequest(http.MethodGet, "/auth/whoami", nil)
	w := httptest.NewRecorder()
	p.handleWhoami(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --- Logout tests ---

func TestLogout_ClearsSession(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	w := httptest.NewRecorder()
	p.handleLogout(w, r)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}
	if loc := w.Header().Get("Location"); loc != "/" {
		t.Errorf("redirect = %q, want %q", loc, "/")
	}

	// Session cookie should be cleared.
	sessionCookie := findCookie(w.Result().Cookies(), cookieName)
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set (for clearing)")
	}
	if sessionCookie.MaxAge != -1 {
		t.Errorf("session cookie MaxAge = %d, want -1", sessionCookie.MaxAge)
	}
}

// --- Bearer token tests via Authenticate ---

func TestAuthenticate_ValidBearerToken(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	token := idp.issueIDToken(t, "", time.Now().Add(time.Hour))

	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil {
		t.Fatal("expected identity, got nil")
	}
	if id.Subject != "user-42" {
		t.Errorf("Subject = %q, want %q", id.Subject, "user-42")
	}
}

func TestAuthenticate_ExpiredBearerToken(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	token := idp.issueIDToken(t, "", time.Now().Add(-time.Hour))

	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err == nil {
		t.Fatal("expected error for expired bearer token")
	}
	if id != nil {
		t.Error("expected nil identity")
	}

	var authErr *AuthError
	if !errorAs(err, &authErr) {
		t.Fatalf("expected AuthError, got %T: %v", err, err)
	}
	if authErr.WWWAuthenticate != `Bearer error="invalid_token"` {
		t.Errorf("WWW-Authenticate = %q, want %q", authErr.WWWAuthenticate, `Bearer error="invalid_token"`)
	}
}

func TestAuthenticate_InvalidBearerToken(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.Header.Set("Authorization", "Bearer not-a-valid-jwt")
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err == nil {
		t.Fatal("expected error for invalid bearer token")
	}
	if id != nil {
		t.Error("expected nil identity")
	}
}

// --- Helpers ---

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// errorAs is a generic wrapper to avoid importing errors in the test.
func errorAs(err error, target any) bool {
	// Use type assertion pattern matching AuthError specifically.
	switch t := target.(type) {
	case **AuthError:
		for err != nil {
			if ae, ok := err.(*AuthError); ok {
				*t = ae
				return true
			}
			// No wrapping in AuthError, so this is sufficient.
			return false
		}
	}
	return false
}

