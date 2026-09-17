package oauth

import (
	"bytes"
	"html"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"

	"github.com/radiergummi/cetacean/internal/spec"
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
	spec.Satisfies(t,
		"oauth/rfc8252/no-silent-authorization",
		"oauth/rfc9700/clickjacking-prevented",
		"oauth/rfc9700/csp-used-against-framing",
		"oauth/rfc9700/csp-combined-with-a-legacy-defence",
	)

	s := newTestServer(t)
	challenge := computeS256Challenge("verifier")
	clientID := registeredClient(t, s, []string{"http://localhost:9999/cb"})

	rawURL := authorizeURL(
		clientID,
		"http://localhost:9999/cb",
		challenge,
		"state123",
		s.resources.fallback,
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

	// The literal name, not the constant: a consent form in flight is matched by
	// what is on the wire, so renaming the constant costs a re-prompt and must
	// be a visible decision rather than a silent one.
	var names []string
	for _, cookie := range rec.Result().Cookies() {
		names = append(names, cookie.Name)
	}
	if !slices.Contains(names, "oauth_csrf_nonce") {
		t.Errorf("cookies = %q, want one named oauth_csrf_nonce", names)
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
		s.resources.fallback,
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
		s.resources.fallback,
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

// consentForm rebuilds the form a browser would resubmit from a rendered consent
// page: every hidden input the template emitted, plus the decision. Reading the
// fields back off the page rather than restating them keeps these tests in step
// with the template, and carries a newly added field automatically.
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

// The consent page must not accept a token signed with the root itself, which
// is what it did before the keys were derived.
func TestConsentRefusesACSRFTokenSignedWithTheRoot(t *testing.T) {
	km := mustDeriveKeys(t, testRoot)

	if bytes.Equal(km.csrf, testRoot) {
		t.Fatal("derived CSRF key is the root; the split did not happen")
	}

	const (
		nonce       = "test-nonce"
		state       = "test-state"
		fingerprint = "test-fingerprint"
	)

	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(
		url.Values{
			"state":                 {state},
			consentFingerprintField: {fingerprint},
			"csrf_token": {csrfMAC(testRoot, nonce, consentBinding{
				State:       state,
				Fingerprint: fingerprint,
			})},
		}.Encode(),
	))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: nonce})

	if verifyCSRFToken(req, km.csrf) {
		t.Error("a CSRF token signed with the root verified against the derived key")
	}
}

// A token verifies for the request it was issued against and no other. Swapping
// any field the page carried in a hidden input has to break it, or the consent
// the user gave would cover a request they never saw.
func TestConsentTokenIsBoundToTheWholeRequest(t *testing.T) {
	km := mustDeriveKeys(t, testRoot)

	const nonce = "test-nonce"
	issued := consentBinding{
		State:               "test-state",
		Fingerprint:         "test-fingerprint",
		ClientID:            "https://client.example/id",
		RedirectURI:         "https://client.example/callback",
		CodeChallenge:       computeS256Challenge(authorizeVerifier),
		CodeChallengeMethod: "S256",
		ResponseType:        "code",
		Resource:            "https://swarm.example.com/first",
	}
	token := csrfMAC(km.csrf, nonce, issued)

	submit := func(b consentBinding) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(
			url.Values{
				"state":                 {b.State},
				consentFingerprintField: {b.Fingerprint},
				"client_id":             {b.ClientID},
				"redirect_uri":          {b.RedirectURI},
				"code_challenge":        {b.CodeChallenge},
				"code_challenge_method": {b.CodeChallengeMethod},
				"response_type":         {b.ResponseType},
				"resource":              {b.Resource},
				"csrf_token":            {token},
			}.Encode(),
		))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: nonce})

		return req
	}

	if !verifyCSRFToken(submit(issued), km.csrf) {
		t.Fatal("the request it was issued for did not verify")
	}

	swapped := map[string]func(*consentBinding){
		"state":        func(b *consentBinding) { b.State = "other-state" },
		"fingerprint":  func(b *consentBinding) { b.Fingerprint = "other-fingerprint" },
		"client_id":    func(b *consentBinding) { b.ClientID = "https://attacker.example/id" },
		"redirect_uri": func(b *consentBinding) { b.RedirectURI = "https://attacker.example/cb" },
		"code_challenge": func(b *consentBinding) {
			b.CodeChallenge = computeS256Challenge("a" + authorizeVerifier)
		},
		"code_challenge_method": func(b *consentBinding) { b.CodeChallengeMethod = "plain" },
		"response_type":         func(b *consentBinding) { b.ResponseType = "token" },
		"resource":              func(b *consentBinding) { b.Resource = "https://swarm.example.com/second" },
	}

	for field, swap := range swapped {
		t.Run(field, func(t *testing.T) {
			tampered := issued
			swap(&tampered)

			if verifyCSRFToken(submit(tampered), km.csrf) {
				t.Errorf("a token issued for another %s verified", field)
			}
		})
	}
}

// Length-prefixing is what stops one field's content spelling the next one's.
// Without it a client-chosen state could carry the fingerprint's bytes and a
// token issued for one pair would verify for a different one.
func TestConsentTokenFieldsCannotSpellEachOther(t *testing.T) {
	km := mustDeriveKeys(t, testRoot)

	const nonce = "test-nonce"

	left := csrfMAC(km.csrf, nonce, consentBinding{State: "ab", Fingerprint: "cd"})
	right := csrfMAC(km.csrf, nonce, consentBinding{State: "a", Fingerprint: "bcd"})

	if left == right {
		t.Error("fields ran together; the MAC does not length-prefix them")
	}
}

// ---------------------------------------------------------------------------
// TestConsentPageNamesTheResourceBeingAuthorized
// ---------------------------------------------------------------------------

// A server offering the deployment root and one resource mounted beneath it.
// The two are separate audiences: a token for one does not reach the other.
func newTwoResourceServer(t *testing.T) *Server {
	t.Helper()

	s := NewServer(ServerConfig{
		Issuer: "https://cetacean.test",
		Resources: []Resource{
			{Realm: "cetacean"},
			{Path: "/other", Realm: "cetacean-other"},
		},
		OAuth: config.OAuthConfig{
			AccessTokenTTL: time.Hour,
			ConsentTTL:     testConsentTTL,
			DCREnabled:     true,
			DCRRateLimit:   10,
			DCRMaxClients:  100,
			CIMDEnabled:    true,
		},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	})
	s.cimd.AllowLoopback = true

	return s
}

// grantPattern matches the first warning block on the consent page: the one
// that says what approving actually hands over.
var grantPattern = regexp.MustCompile(`(?s)<div class="warning">(.*?)</div>`)

// grantDisclosure reads that block back as flat text.
func grantDisclosure(t *testing.T, page string) string {
	t.Helper()

	match := grantPattern.FindStringSubmatch(page)
	if match == nil {
		t.Fatal("consent page has no grant disclosure")
	}

	return html.UnescapeString(strings.Join(strings.Fields(match[1]), " "))
}

// consentPageFor renders the consent page for one resource identifier.
func consentPageFor(t *testing.T, s *Server, resource string) string {
	t.Helper()

	const redirectURI = "http://localhost:8711/cb"
	target := authorizeURL(
		registeredClient(t, s, []string{redirectURI}),
		redirectURI,
		computeS256Challenge("verifier-padded-to-the-RFC-7636-minimum-length"),
		"state",
		resource,
	)

	rec := httptest.NewRecorder()
	s.HandleAuthorize(rec, withIdentity(
		httptest.NewRequest(http.MethodGet, target, nil),
		"alice",
		"",
	))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET consent for %s: %d: %s", resource, rec.Code, rec.Body.String())
	}

	return rec.Body.String()
}

// Splitting the audiences buys nothing if the page asking for one reads exactly
// like the page asking for the other: the person clicking Approve is the only
// check on which resource a client walks away with.
func TestConsentPageNamesTheResourceBeingAuthorized(t *testing.T) {
	s := newTwoResourceServer(t)
	root, sub := s.resources.identifiers[0], s.resources.identifiers[1]

	rootGrant := grantDisclosure(t, consentPageFor(t, s, root))
	subGrant := grantDisclosure(t, consentPageFor(t, s, sub))

	if !strings.Contains(subGrant, sub) {
		t.Errorf("the disclosure does not name the resource: %s", subGrant)
	}
	if strings.Contains(rootGrant, "/other") {
		t.Errorf("the root's disclosure names another resource: %s", rootGrant)
	}
	if rootGrant == subGrant {
		t.Errorf("both resources ask for the same thing: %s", rootGrant)
	}
}

// RFC 7591 §5 requires client metadata to be treated as self-asserted: a rogue
// client can register any name it likes. The consent page says so, and an
// approval it wins is never remembered.
func TestADynamicallyRegisteredClientIsLabelledSelfAsserted(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc7591/metadata-is-self-asserted")

	s := newTestServer(t)
	challenge := computeS256Challenge("verifier")
	clientID := registeredClient(t, s, []string{"http://localhost:9999/cb"})

	rawURL := authorizeURL(
		clientID,
		"http://localhost:9999/cb",
		challenge,
		"state123",
		s.resources.fallback,
	)
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	req = withIdentity(req, "alice", "alice@example.com")
	rec := httptest.NewRecorder()

	s.HandleAuthorize(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Self-registered") {
		t.Error("a self-asserted client name is presented without saying so")
	}
	if strings.Contains(body, `<span class="badge badge-verified">`) {
		t.Error("a dynamically registered client is presented as verified")
	}
	if strings.Contains(body, "will be remembered") {
		t.Error("an approval for a self-asserted client is offered as remembered")
	}
}
