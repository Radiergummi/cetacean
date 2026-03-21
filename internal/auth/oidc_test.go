package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// newMockOIDCServer creates a test server that serves OIDC discovery and JWKS
// endpoints. The issuer URL is the server's own URL (assigned dynamically).
func newMockOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	var server *httptest.Server

	mux.HandleFunc(
		"/.well-known/openid-configuration",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q
		}`, server.URL, server.URL+"/authorize", server.URL+"/token", server.URL+"/jwks")
		},
	)

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"keys":[]}`)
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

func newTestOIDCProvider(t *testing.T, issuerURL string) *OIDCProvider {
	t.Helper()

	p, err := NewOIDCProvider(t.Context(), OIDCProviderConfig{
		Issuer:       issuerURL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/auth/callback",
		Scopes:       []string{"openid", "profile", "email"},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	return p
}

func TestOIDCProvider_Authenticate_BrowserRedirect(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != nil {
		t.Fatal("expected nil identity for redirect, got non-nil")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("expected Location header on redirect")
	}

	// Should have set state, nonce, verifier, and redirect cookies — all with Secure flag.
	var hasState, hasNonce, hasVerifier, hasRedirect bool
	for _, c := range resp.Cookies() {
		switch c.Name {
		case "cetacean_auth_state":
			hasState = true
			if !c.Secure {
				t.Error("state cookie missing Secure flag")
			}
		case "cetacean_auth_nonce":
			hasNonce = true
			if !c.Secure {
				t.Error("nonce cookie missing Secure flag")
			}
		case "cetacean_auth_verifier":
			hasVerifier = true
			if !c.Secure {
				t.Error("verifier cookie missing Secure flag")
			}
			if !c.HttpOnly {
				t.Error("verifier cookie missing HttpOnly flag")
			}
		case "cetacean_auth_redirect":
			hasRedirect = true
			if !c.Secure {
				t.Error("redirect cookie missing Secure flag")
			}
		}
	}
	if !hasState {
		t.Error("missing cetacean_auth_state cookie")
	}
	if !hasNonce {
		t.Error("missing cetacean_auth_nonce cookie")
	}
	if !hasVerifier {
		t.Error("missing cetacean_auth_verifier cookie")
	}
	if !hasRedirect {
		t.Error("missing cetacean_auth_redirect cookie")
	}

	// Verify the authorize URL includes nonce and PKCE parameters.
	if !strings.Contains(location, "nonce=") {
		t.Error("authorize URL missing nonce parameter")
	}
	if !strings.Contains(location, "code_challenge=") {
		t.Error("authorize URL missing code_challenge parameter")
	}
	if !strings.Contains(location, "code_challenge_method=S256") {
		t.Error("authorize URL missing code_challenge_method=S256")
	}
}

func TestOIDCProvider_Authenticate_APIError(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err == nil {
		t.Fatal("expected error for unauthenticated API request")
	}
	if id != nil {
		t.Fatal("expected nil identity")
	}
	if err.Error() != "authentication required" {
		t.Errorf("error = %q, want %q", err.Error(), "authentication required")
	}
}

func TestOIDCProvider_Authenticate_ValidSession(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	// Create a session cookie by writing to a recorder.
	rec := httptest.NewRecorder()
	testID := &Identity{
		Subject:     "user-123",
		DisplayName: "Test User",
		Email:       "test@example.com",
		Provider:    "oidc",
	}
	p.session.Set(rec, testID, maxSessionTTL)

	// Extract the cookie and add it to a new request.
	sessionCookie := rec.Result().Cookies()[0]
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.AddCookie(sessionCookie)
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil {
		t.Fatal("expected identity from session, got nil")
	}
	if id.Subject != "user-123" {
		t.Errorf("Subject = %q, want %q", id.Subject, "user-123")
	}
	if id.DisplayName != "Test User" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Test User")
	}
}

func TestOIDCProvider_RegisterRoutes_Login(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("expected Location header")
	}

	// Should have set state, nonce, and verifier cookies.
	var hasState, hasNonce, hasVerifier bool
	for _, c := range resp.Cookies() {
		switch c.Name {
		case "cetacean_auth_state":
			hasState = true
		case "cetacean_auth_nonce":
			hasNonce = true
		case "cetacean_auth_verifier":
			hasVerifier = true
		}
	}
	if !hasState {
		t.Error("missing cetacean_auth_state cookie")
	}
	if !hasNonce {
		t.Error("missing cetacean_auth_nonce cookie")
	}
	if !hasVerifier {
		t.Error("missing cetacean_auth_verifier cookie")
	}

	// Verify PKCE parameters in authorize URL.
	if !strings.Contains(location, "code_challenge=") {
		t.Error("authorize URL missing code_challenge parameter")
	}
}

func TestClaimsToIdentity(t *testing.T) {
	claims := map[string]any{
		"sub":    "user-456",
		"name":   "Jane Doe",
		"email":  "jane@example.com",
		"groups": []any{"admin", "dev"},
	}

	id := claimsToIdentity(claims)

	if id.Subject != "user-456" {
		t.Errorf("Subject = %q, want %q", id.Subject, "user-456")
	}
	if id.DisplayName != "Jane Doe" {
		t.Errorf("DisplayName = %q, want %q", id.DisplayName, "Jane Doe")
	}
	if id.Email != "jane@example.com" {
		t.Errorf("Email = %q, want %q", id.Email, "jane@example.com")
	}
	if len(id.Groups) != 2 || id.Groups[0] != "admin" || id.Groups[1] != "dev" {
		t.Errorf("Groups = %v, want [admin dev]", id.Groups)
	}
	if id.Provider != "oidc" {
		t.Errorf("Provider = %q, want %q", id.Provider, "oidc")
	}
}

func TestClaimsToIdentity_MissingFields(t *testing.T) {
	// Empty claims map — all fields should be zero-valued.
	id := claimsToIdentity(map[string]any{})

	if id.Subject != "" {
		t.Errorf("Subject = %q, want empty", id.Subject)
	}
	if id.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty", id.DisplayName)
	}
	if id.Email != "" {
		t.Errorf("Email = %q, want empty", id.Email)
	}
	if len(id.Groups) != 0 {
		t.Errorf("Groups = %v, want empty", id.Groups)
	}
	if id.Provider != "oidc" {
		t.Errorf("Provider = %q, want %q", id.Provider, "oidc")
	}
}

func TestClaimsToIdentity_GroupsWithNonStrings(t *testing.T) {
	// Groups array containing non-string elements should be skipped.
	claims := map[string]any{
		"sub":    "user-1",
		"groups": []any{"admin", 42, true, nil, "dev"},
	}

	id := claimsToIdentity(claims)

	if len(id.Groups) != 2 || id.Groups[0] != "admin" || id.Groups[1] != "dev" {
		t.Errorf("Groups = %v, want [admin dev]", id.Groups)
	}
}

func TestClaimsToIdentity_WrongTypes(t *testing.T) {
	// Claims with wrong types should be handled gracefully.
	claims := map[string]any{
		"sub":    42,      // number instead of string
		"name":   true,    // bool instead of string
		"email":  []any{}, // array instead of string
		"groups": "not-an-array",
	}

	id := claimsToIdentity(claims)

	// All should be zero-valued since type assertions fail.
	if id.Subject != "" {
		t.Errorf("Subject = %q, want empty", id.Subject)
	}
	if id.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty", id.DisplayName)
	}
	if id.Email != "" {
		t.Errorf("Email = %q, want empty", id.Email)
	}
	if len(id.Groups) != 0 {
		t.Errorf("Groups = %v, want empty", id.Groups)
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", "abc123"},
		{"BEARER abc123", "abc123"},
		{"Basic abc123", ""},
		{"", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
	}

	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if tt.header != "" {
			r.Header.Set("Authorization", tt.header)
		}
		got := extractBearerToken(r)
		if got != tt.want {
			t.Errorf("extractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestIsRelativePath(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/", true},
		{"/nodes", true},
		{"/nodes?sort=name", true},
		{"", false},
		{"//evil.com", false},
		{"https://evil.com", false},
		{"javascript:alert(1)", false},
		{"/path\\segment", false},
		{"/\\evil.com", false},
		{"/a\\b\\c", false},
		{"\\evil.com", false},
		{"/%5cevil.com", true}, // encoded backslash stays encoded, safe
	}
	for _, tt := range tests {
		if got := isRelativePath(tt.input); got != tt.want {
			t.Errorf("isRelativePath(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestGenerateState(t *testing.T) {
	s := generateState()
	if len(s) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("state length = %d, want 32", len(s))
	}

	// Should be unique.
	s2 := generateState()
	if s == s2 {
		t.Error("two generateState calls returned same value")
	}
}

// ---------------------------------------------------------------------------
// Authenticate edge cases
// ---------------------------------------------------------------------------

func TestOIDCProvider_Authenticate_ExpiredSession(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	// Create a valid session.
	rec := httptest.NewRecorder()
	p.session.Set(rec, &Identity{Subject: "user-123", Provider: "oidc"}, time.Hour)
	sessionCookie := rec.Result().Cookies()[0]

	// Advance clock past expiry.
	p.session.now = func() time.Time { return time.Now().Add(2 * time.Hour) }

	// API request: should get an auth error.
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.AddCookie(sessionCookie)
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err == nil {
		t.Fatal("expected error for expired session")
	}
	if id != nil {
		t.Fatal("expected nil identity")
	}

	// Browser request: should redirect to login.
	r = httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.AddCookie(sessionCookie)
	r.Header.Set("Accept", "text/html")
	w = httptest.NewRecorder()

	id, err = p.Authenticate(w, r)
	if err != nil {
		t.Fatalf("browser request shouldn't error: %v", err)
	}
	if id != nil {
		t.Fatal("expected nil identity for redirect")
	}
	if w.Result().StatusCode != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Result().StatusCode, http.StatusFound)
	}
}

func TestOIDCProvider_Authenticate_TamperedSession(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "tampered.garbage"})
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err == nil {
		t.Fatal("expected error for tampered session")
	}
	if id != nil {
		t.Fatal("expected nil identity")
	}
}

// ---------------------------------------------------------------------------
// AZP validation (OIDC Core Section 3.1.3.7)
// ---------------------------------------------------------------------------

func TestValidateAzp_SingleAudience(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	token := &oidc.IDToken{Audience: []string{"test-client"}}
	if err := p.validateAzp(token, map[string]any{}); err != nil {
		t.Errorf("single audience should pass: %v", err)
	}
}

func TestValidateAzp_MultiAudience_ValidAzp(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	token := &oidc.IDToken{Audience: []string{"test-client", "other-client"}}
	claims := map[string]any{"azp": "test-client"}
	if err := p.validateAzp(token, claims); err != nil {
		t.Errorf("valid azp should pass: %v", err)
	}
}

func TestValidateAzp_MultiAudience_MissingAzp(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	token := &oidc.IDToken{Audience: []string{"test-client", "other-client"}}
	if err := p.validateAzp(token, map[string]any{}); err == nil {
		t.Error("missing azp should fail for multi-audience token")
	}
}

func TestValidateAzp_MultiAudience_WrongAzp(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	token := &oidc.IDToken{Audience: []string{"test-client", "other-client"}}
	claims := map[string]any{"azp": "wrong-client"}
	if err := p.validateAzp(token, claims); err == nil {
		t.Error("wrong azp should fail")
	}
}

// ---------------------------------------------------------------------------
// Login redirect safety tests
// ---------------------------------------------------------------------------

func TestLogin_RejectsAbsoluteURL(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	r := httptest.NewRequest(http.MethodGet, "/auth/login?redirect=https://evil.com/steal", nil)
	w := httptest.NewRecorder()
	p.handleLogin(w, r)

	// Should redirect to IdP, but the redirect cookie should contain "/"
	// (the fallback), not the absolute URL.
	for _, c := range w.Result().Cookies() {
		if c.Name == "cetacean_auth_redirect" {
			if c.Value != "/" {
				t.Errorf(
					"redirect cookie = %q, want %q (absolute URL should be rejected)",
					c.Value,
					"/",
				)
			}
			return
		}
	}
	t.Fatal("missing cetacean_auth_redirect cookie")
}

func TestLogin_RejectsProtocolRelativeURL(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	r := httptest.NewRequest(http.MethodGet, "/auth/login?redirect=//evil.com/steal", nil)
	w := httptest.NewRecorder()
	p.handleLogin(w, r)

	for _, c := range w.Result().Cookies() {
		if c.Name == "cetacean_auth_redirect" {
			if c.Value != "/" {
				t.Errorf(
					"redirect cookie = %q, want %q (protocol-relative URL should be rejected)",
					c.Value,
					"/",
				)
			}
			return
		}
	}
	t.Fatal("missing cetacean_auth_redirect cookie")
}

func TestLogin_RejectsBackslashURL(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)

	r := httptest.NewRequest(http.MethodGet, "/auth/login?redirect=/path\\evil", nil)
	w := httptest.NewRecorder()
	p.handleLogin(w, r)

	for _, c := range w.Result().Cookies() {
		if c.Name == "cetacean_auth_redirect" {
			if c.Value != "/" {
				t.Errorf(
					"redirect cookie = %q, want %q (backslash URL should be rejected)",
					c.Value,
					"/",
				)
			}
			return
		}
	}
	t.Fatal("missing cetacean_auth_redirect cookie")
}

func TestCallback_RejectsAbsoluteRedirectCookie(t *testing.T) {
	idp := newMockIDP(t, "test-client")
	p := newProviderWithIDP(t, idp, "http://localhost/auth/callback")

	cookies, state, nonce, _ := initiateLogin(t, p)

	idToken := idp.issueIDToken(t, nonce, time.Now().Add(time.Hour))
	idp.setTokenHandler(t, idToken, time.Now().Add(time.Hour))

	// Tamper with the redirect cookie to contain an absolute URL.
	var tamperedCookies []*http.Cookie
	for _, c := range cookies {
		if c.Name == "cetacean_auth_redirect" {
			c.Value = "https://evil.com/steal"
		}
		tamperedCookies = append(tamperedCookies, c)
	}

	query := url.Values{"code": {"code"}, "state": {state}}
	r := buildCallbackRequest(tamperedCookies, query)
	w := httptest.NewRecorder()
	p.handleCallback(w, r)

	// Should succeed but redirect to "/" (the fallback), not the tampered URL.
	if loc := w.Result().Header.Get("Location"); loc != "/" {
		t.Errorf("redirect = %q, want %q (absolute URL in cookie should be rejected)", loc, "/")
	}
}

// authFlowCookieNames are the cookies set during the OIDC login redirect
// that must be cleared on every callback exit path.
var authFlowCookieNames = []string{
	"cetacean_auth_state",
	"cetacean_auth_nonce",
	"cetacean_auth_verifier",
	"cetacean_auth_redirect",
}

// assertAuthFlowCookiesCleared verifies that all auth flow cookies are deleted
// (MaxAge == -1) in the response.
func assertAuthFlowCookiesCleared(t *testing.T, resp *http.Response) {
	t.Helper()
	cleared := make(map[string]bool)
	for _, c := range resp.Cookies() {
		if c.MaxAge == -1 {
			cleared[c.Name] = true
		}
	}
	for _, name := range authFlowCookieNames {
		if !cleared[name] {
			t.Errorf("auth flow cookie %q was not cleared (MaxAge=-1)", name)
		}
	}
}

// addAuthFlowCookies adds all four auth flow cookies to a request, simulating
// a browser returning from the IdP authorization endpoint.
func addAuthFlowCookies(r *http.Request, state, nonce, verifier, redirect string) {
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_state", Value: state})
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_nonce", Value: nonce})
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_verifier", Value: verifier})
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_redirect", Value: redirect})
}

func TestCallback_IdPError_ClearsCookies(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(
		http.MethodGet,
		"/auth/callback?error=access_denied&error_description=user+denied",
		nil,
	)
	addAuthFlowCookies(r, "some-state", "some-nonce", "some-verifier", "/nodes")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	assertAuthFlowCookiesCleared(t, resp)
}

func TestCallback_IssuerMismatch_ClearsCookies(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(
		http.MethodGet,
		"/auth/callback?code=abc&state=test-state&iss=https://evil.example.com",
		nil,
	)
	addAuthFlowCookies(r, "test-state", "some-nonce", "some-verifier", "/")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	assertAuthFlowCookiesCleared(t, resp)
}

func TestCallback_MissingStateCookie_ClearsCookies(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state=test-state", nil)
	// Deliberately omit the state cookie; add the others.
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_nonce", Value: "some-nonce"})
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_verifier", Value: "some-verifier"})
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_redirect", Value: "/"})
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	assertAuthFlowCookiesCleared(t, resp)
}

func TestCallback_StateMismatch_ClearsCookies(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state=wrong-state", nil)
	addAuthFlowCookies(r, "correct-state", "some-nonce", "some-verifier", "/")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	assertAuthFlowCookiesCleared(t, resp)
}

func TestCallback_MissingNonceCookie_ClearsCookies(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	state := "matching-state"
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+state, nil)
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_state", Value: state})
	// Deliberately omit nonce cookie.
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_verifier", Value: "some-verifier"})
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_redirect", Value: "/"})
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	assertAuthFlowCookiesCleared(t, resp)
}

func TestCallback_MissingVerifierCookie_ClearsCookies(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	state := "matching-state"
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+state, nil)
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_state", Value: state})
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_nonce", Value: "some-nonce"})
	// Deliberately omit verifier cookie.
	r.AddCookie(&http.Cookie{Name: "cetacean_auth_redirect", Value: "/"})
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	assertAuthFlowCookiesCleared(t, resp)
}

func TestCallback_TokenExchangeFails_ClearsCookies(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	// Token exchange will fail because the mock server has no /token endpoint.
	state := "matching-state"
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?code=invalid-code&state="+state, nil)
	addAuthFlowCookies(r, state, "some-nonce", "some-verifier", "/services")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	assertAuthFlowCookiesCleared(t, resp)
}

func TestLogout_SameOrigin_ClearsSession(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Host = "app.example.com"
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want %q", loc, "/")
	}

	// Session cookie should be cleared.
	var sessionCleared bool
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && c.MaxAge == -1 {
			sessionCleared = true
		}
	}
	if !sessionCleared {
		t.Error("session cookie was not cleared")
	}
}

func TestLogout_CrossSite_Rejected(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Host = "app.example.com"
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestLogout_CrossOriginHeader_Rejected(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	// No Sec-Fetch-Site, but Origin mismatches Host — falls back to Origin check.
	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	r.Header.Set("Origin", "https://evil.example.com")
	r.Host = "app.example.com"
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestLogout_MatchingOriginHeader_Accepted(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	// No Sec-Fetch-Site, but Origin matches Host.
	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	r.Header.Set("Origin", "https://app.example.com")
	r.Host = "app.example.com"
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
}

func TestLogout_NonBrowserClient_Accepted(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	// Non-browser clients send neither Sec-Fetch-Site nor Origin.
	// CrossOriginProtection allows these through since CSRF is a browser attack.
	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	r.Host = "app.example.com"
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
}

func TestLogout_GETMethod_Rejected(t *testing.T) {
	server := newMockOIDCServer(t)
	p := newTestOIDCProvider(t, server.URL)
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	r.Host = "app.example.com"
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	resp := w.Result()
	// Go 1.22+ mux returns 405 Method Not Allowed for wrong method.
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}
