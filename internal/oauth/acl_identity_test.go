package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

// The grant docs/authorization.md uses as its own example. Nothing in it names
// a credential, so it must decide the same way for every credential one person
// can hold.
func emailKeyedPolicy() *acl.Policy {
	return &acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:*"},
		Audience:    []string{"user:*@example.com"},
		Permissions: []string{"read"},
	}}}
}

// A subject that is not an address is the case an email-keyed grant exists to
// serve: match on sub and the grant would need no email at all.
const (
	fixtureSubject = "a3f1c8e2-7b04-4d19-9e55-2c6f0b8a41d7"
	fixtureEmail   = "alice@example.com"
)

// mintTokenForIdentity drives the whole flow a client drives — consent under a
// session, then the code grant — so the token carries whatever the server
// actually propagates from the identity rather than what a test hands it.
func mintTokenForIdentity(t *testing.T, s *Server, subject, email string) string {
	t.Helper()

	const (
		redirectURI = "http://localhost:8611/cb"
		verifier    = "identity-fidelity-verifier-padded-to-the-RFC-7636-minimum"
	)
	clientID := registeredClient(t, s, []string{redirectURI})
	challenge := computeS256Challenge(verifier)

	page := httptest.NewRecorder()
	s.HandleAuthorize(page, withIdentity(
		httptest.NewRequest(
			http.MethodGet,
			authorizeURL(clientID, redirectURI, challenge, "state", s.cfg.Resource),
			nil,
		),
		subject, email,
	))
	if page.Code != http.StatusOK {
		t.Fatalf("GET consent: %d: %s", page.Code, page.Body.String())
	}

	approved := submitConsent(t, s, page, "approve", subject, email, nil)
	if approved.Code != http.StatusFound {
		t.Fatalf("POST consent: %d: %s", approved.Code, approved.Body.String())
	}

	loc, err := url.Parse(approved.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect: %s", approved.Header().Get("Location"))
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token exchange: %d: %s", rec.Code, rec.Body.String())
	}

	var resp tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}

	return resp.AccessToken
}

// A grant is written against a person, not against a credential. The same human
// over a session and over a token must reach the same answer, or authorization
// depends on which client they happened to pick.
func TestATokenReachesTheSameGrantAsASession(t *testing.T) {
	s := newTestServer(t)
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(emailKeyedPolicy())

	session := &auth.Identity{
		Subject:  fixtureSubject,
		Email:    fixtureEmail,
		Provider: "oidc",
	}
	if !evaluator.Can(session, "read", "service:web") {
		t.Fatal("the session identity misses the grant; the fixture is wrong")
	}

	fromToken, err := s.Identify(mintTokenForIdentity(t, s, fixtureSubject, fixtureEmail))
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if !evaluator.Can(fromToken, "read", "service:web") {
		t.Errorf("a token from the same person is denied what their session is allowed: %+v",
			fromToken)
	}
}

// The ACL assertion above would also pass on a token that carried the email
// and lost the subject, so pin both fields against the flow that set them.
func TestATokenCarriesTheIdentityTheProviderEstablished(t *testing.T) {
	s := newTestServer(t)

	fromToken, err := s.Identify(mintTokenForIdentity(t, s, fixtureSubject, fixtureEmail))
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if fromToken.Email != fixtureEmail {
		t.Errorf("email = %q, want %q", fromToken.Email, fixtureEmail)
	}
	if fromToken.Subject != fixtureSubject {
		t.Errorf("subject = %q, want %q", fromToken.Subject, fixtureSubject)
	}
}
