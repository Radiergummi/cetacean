package oauth

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// runConsent drives the full authorize flow — GET the consent page for its CSRF
// token and nonce cookie, then POST the decision — and returns the redirect the
// server answered with. decision is "approve" or "deny".
func runConsent(t *testing.T, s *Server, decision string, overrides url.Values) *url.URL {
	t.Helper()

	const redirectURI = "http://localhost:7777/cb"

	challenge := computeS256Challenge("verifier-issuer")
	clientID := registeredClient(t, s, []string{redirectURI})

	getReq := httptest.NewRequest(
		http.MethodGet,
		authorizeURL(clientID, redirectURI, challenge, "stateISS", s.cfg.MCPResource),
		nil,
	)
	getReq = withIdentity(getReq, "bob", "bob@example.com")

	getRec := httptest.NewRecorder()
	s.HandleAuthorize(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET consent: status %d: %s", getRec.Code, getRec.Body.String())
	}

	csrfToken := extractHiddenField(getRec.Body.String(), "csrf_token")
	if csrfToken == "" {
		t.Fatal("CSRF token not found in consent page")
	}

	// The CSRF token is bound to the fingerprint of the metadata the page was
	// rendered from, so a browser resubmitting the form must carry it back.
	fingerprint := extractHiddenField(getRec.Body.String(), consentFingerprintField)
	if fingerprint == "" {
		t.Fatal("metadata fingerprint not found in consent page")
	}

	var nonceCookie *http.Cookie
	for _, cookie := range getRec.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			nonceCookie = cookie

			break
		}
	}

	if nonceCookie == nil {
		t.Fatal("CSRF nonce cookie not set")
	}

	form := url.Values{
		"decision":              {decision},
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"stateISS"},
		"resource":              {s.cfg.MCPResource},
		"csrf_token":            {csrfToken},
		consentFingerprintField: {fingerprint},
	}

	maps.Copy(form, overrides)

	postReq := httptest.NewRequest(
		http.MethodPost,
		"/oauth/authorize",
		strings.NewReader(form.Encode()),
	)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(nonceCookie)
	postReq = withIdentity(postReq, "bob", "bob@example.com")

	postRec := httptest.NewRecorder()
	s.HandleAuthorize(postRec, postReq)

	if postRec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d: %s", postRec.Code, postRec.Body.String())
	}

	location, err := url.Parse(postRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}

	return location
}

// TestAuthorizeResponseCarriesIssuer — RFC 9207. Without iss, a client with
// several authorization servers configured cannot tell which one issued the
// code it just received, which is what enables a mix-up attack.
func TestAuthorizeResponseCarriesIssuer(t *testing.T) {
	s := newTestServer(t)

	location := runConsent(t, s, "approve", nil)
	if location.Query().Get("code") == "" {
		t.Fatalf("expected an authorization code, got %s", location)
	}

	got := location.Query().Get("iss")
	if got == "" {
		t.Fatal("authorization response omits iss (RFC 9207)")
	}

	if want := s.cfg.issuerID(); got != want {
		t.Fatalf("iss = %q, want %q", got, want)
	}
}

// TestAuthorizeErrorResponseCarriesIssuer — RFC 9207 §2 requires iss on error
// redirects too, for the same reason: the client must be able to attribute the
// response before acting on it.
func TestAuthorizeErrorResponseCarriesIssuer(t *testing.T) {
	s := newTestServer(t)

	location := runConsent(t, s, "deny", nil)
	if location.Query().Get("error") == "" {
		t.Fatalf("expected an error redirect, got %s", location)
	}

	if got, want := location.Query().Get("iss"), s.cfg.issuerID(); got != want {
		t.Fatalf("iss on error redirect = %q, want %q", got, want)
	}
}
