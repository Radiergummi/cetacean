package oauth

import (
	"html"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
)

// registeredClient registers a DCR client in the server's registry and
// returns the client_id.
func registeredClient(t *testing.T, s *Server, redirectURIs []string) string {
	t.Helper()
	reg := &ClientRegistration{
		ClientID:                "cetacean-testclient",
		ClientName:              "Test App",
		RedirectURIs:            redirectURIs,
		TokenEndpointAuthMethod: "none",
		ClientIDIssuedAt:        time.Now().Unix(),
	}
	s.clients.register(reg)
	return reg.ClientID
}

// authorizeURL builds a GET /oauth/authorize URL with standard test params.
func authorizeURL(clientID, redirectURI, challenge, state, resource string) string {
	u := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"resource":              {resource},
	}
	return "/oauth/authorize?" + u.Encode()
}

// withIdentity returns a copy of r with an auth.Identity in its context.
func withIdentity(r *http.Request, subject, email string) *http.Request {
	id := &auth.Identity{Subject: subject, Email: email}
	return r.WithContext(auth.ContextWithIdentity(r.Context(), id))
}

// ---------------------------------------------------------------------------
// TestConsentPageRender
// ---------------------------------------------------------------------------

func TestConsentPageRender(t *testing.T) {
	s := newTestServer(t)
	challenge := computeS256Challenge("verifier")
	clientID := registeredClient(t, s, []string{"http://localhost:9999/cb"})

	rawURL := authorizeURL(
		clientID,
		"http://localhost:9999/cb",
		challenge,
		"state123",
		s.cfg.MCPResource,
	)
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	req = withIdentity(req, "alice", "alice@example.com")
	rec := httptest.NewRecorder()

	s.HandleAuthorize(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Test App") {
		t.Error("consent page should contain client name")
	}
	if !strings.Contains(body, "alice") {
		t.Error("consent page should contain identity subject")
	}

	xfo := rec.Header().Get("X-Frame-Options")
	if xfo != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", xfo)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q, want frame-ancestors 'none'", csp)
	}
}

// ---------------------------------------------------------------------------
// TestConsentPageRejectsInvalidRedirectURI
// ---------------------------------------------------------------------------

func TestConsentPageRejectsInvalidRedirectURI(t *testing.T) {
	s := newTestServer(t)
	challenge := computeS256Challenge("verifier")
	clientID := registeredClient(t, s, []string{"http://localhost:9999/cb"})

	// Use a redirect_uri NOT in the registered set.
	rawURL := authorizeURL(
		clientID,
		"http://attacker.example.com/steal",
		challenge,
		"state",
		s.cfg.MCPResource,
	)
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	req = withIdentity(req, "alice", "")
	rec := httptest.NewRecorder()

	s.HandleAuthorize(rec, req)

	// Must NOT redirect to the attacker URL.
	if rec.Code == http.StatusFound {
		loc := rec.Header().Get("Location")
		if strings.Contains(loc, "attacker.example.com") {
			t.Fatalf("SECURITY: redirected to unregistered URI: %s", loc)
		}
	}

	// Must respond with a 4xx error (not a redirect).
	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("expected 4xx status for invalid redirect_uri, got %d", rec.Code)
	}

	// Should render an error page instead.
	body := rec.Body.String()
	if strings.Contains(body, "attacker.example.com") {
		t.Error("error page should not echo the attacker URI back")
	}
}

// ---------------------------------------------------------------------------
// TestConsentApproveProducesCode
// ---------------------------------------------------------------------------

func TestConsentApproveProducesCode(t *testing.T) {
	s := newTestServer(t)
	challenge := computeS256Challenge("verifier-approve")
	clientID := registeredClient(t, s, []string{"http://localhost:7777/cb"})

	// Step 1: GET the consent page to get the CSRF token and nonce cookie.
	rawURL := authorizeURL(
		clientID,
		"http://localhost:7777/cb",
		challenge,
		"stateXYZ",
		s.cfg.MCPResource,
	)
	getReq := httptest.NewRequest(http.MethodGet, rawURL, nil)
	getReq = withIdentity(getReq, "bob", "bob@example.com")
	getRec := httptest.NewRecorder()
	s.HandleAuthorize(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("GET consent: expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// Step 2: POST the approval, resubmitting the form the page rendered.
	postRec := submitConsent(t, s, getRec, "approve", "bob", "bob@example.com", nil)

	if postRec.Code != http.StatusFound {
		t.Fatalf("expected redirect (302), got %d: %s", postRec.Code, postRec.Body.String())
	}

	loc := postRec.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header")
	}

	redirected, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	code := redirected.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect: %s", loc)
	}

	// The code must be redeemable.
	data, ok := s.authCodes.Redeem(code)
	if !ok {
		t.Fatal("auth code not redeemable")
	}
	if data.Subject != "bob" {
		t.Errorf("subject = %q, want bob", data.Subject)
	}
}

// hiddenInputPattern matches the hidden inputs the consent template emits.
var hiddenInputPattern = regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)">`)

// hiddenFields reads every hidden input out of a rendered form, undoing the
// HTML escaping the template applied.
func hiddenFields(page string) url.Values {
	fields := url.Values{}
	for _, match := range hiddenInputPattern.FindAllStringSubmatch(page, -1) {
		fields.Set(match[1], html.UnescapeString(match[2]))
	}

	return fields
}

// consentForm rebuilds the form a browser would resubmit from a rendered
// consent page: every hidden input the template emitted, plus the decision.
//
// Reading the fields back off the page rather than restating them is what keeps
// these tests in step with the template. A field added to the form is carried
// automatically — which is exactly what the consent fingerprint was not, having
// cost an edit in all three places that submit this form.
func consentForm(page, decision string, overrides url.Values) url.Values {
	form := hiddenFields(page)
	form.Set("decision", decision)
	maps.Copy(form, overrides)

	return form
}

// submitConsent POSTs a decision back to the authorize endpoint the way a
// browser would: the form the page rendered, carrying the nonce cookie it set.
func submitConsent(
	t *testing.T,
	s *Server,
	page *httptest.ResponseRecorder,
	decision, subject, email string,
	overrides url.Values,
) *httptest.ResponseRecorder {
	t.Helper()

	form := consentForm(page.Body.String(), decision, overrides)

	req := httptest.NewRequest(
		http.MethodPost,
		"/oauth/authorize",
		strings.NewReader(form.Encode()),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	for _, cookie := range page.Result().Cookies() {
		if cookie.Name == csrfCookieName {
			req.AddCookie(cookie)
		}
	}

	req = withIdentity(req, subject, email)

	w := httptest.NewRecorder()
	s.HandleAuthorize(w, req)

	return w
}
