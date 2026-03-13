package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newMockOIDCServer creates a test server that serves OIDC discovery and JWKS
// endpoints. The issuer URL is the server's own URL (assigned dynamically).
func newMockOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	var server *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q
		}`, server.URL, server.URL+"/authorize", server.URL+"/token", server.URL+"/jwks")
	})

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

	// Should have set state and redirect cookies.
	var hasState, hasRedirect bool
	for _, c := range resp.Cookies() {
		if c.Name == "cetacean_auth_state" {
			hasState = true
		}
		if c.Name == "cetacean_auth_redirect" {
			hasRedirect = true
		}
	}
	if !hasState {
		t.Error("missing cetacean_auth_state cookie")
	}
	if !hasRedirect {
		t.Error("missing cetacean_auth_redirect cookie")
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

	// Should have set state cookie.
	var hasState bool
	for _, c := range resp.Cookies() {
		if c.Name == "cetacean_auth_state" {
			hasState = true
		}
	}
	if !hasState {
		t.Error("missing cetacean_auth_state cookie")
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

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc123", "abc123"},
		{"bearer abc123", ""},
		{"Basic abc123", ""},
		{"", ""},
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
