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
func mintTokenForIdentity(t *testing.T, s *Server) string {
	t.Helper()

	resp, _ := mintTokensForIdentity(t, s)

	return resp.AccessToken
}

// mintTokensForIdentity is mintTokenForIdentity keeping the refresh token, and
// the client it was issued to, for a test that refreshes.
func mintTokensForIdentity(t *testing.T, s *Server) (tokenResponse, string) {
	t.Helper()

	subject, email := fixtureSubject, fixtureEmail

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
			authorizeURL(clientID, redirectURI, challenge, "state", s.resources.fallback),
			nil,
		),
		subject, email,
	))
	if page.Code != http.StatusOK {
		t.Fatalf("GET consent: %d: %s", page.Code, page.Body.String())
	}

	approved := submitConsent(t, s, page, "approve", subject, email, nil)
	if approved.Code != http.StatusSeeOther {
		t.Fatalf("POST consent: %d: %s", approved.Code, approved.Body.String())
	}

	code := redirectedCode(t, approved)
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

	return resp, clientID
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

	fromToken, err := s.Identify(
		mintTokenForIdentity(t, s),
		s.resources.fallback,
	)
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

	fromToken, err := s.Identify(
		mintTokenForIdentity(t, s),
		s.resources.fallback,
	)
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

// fixtureSource grants what a provider would hand fixtureSubject through a
// header or capability its session carries and its token does not.
type fixtureSource struct{}

func (fixtureSource) GrantsFor(id *auth.Identity) []acl.Grant {
	if id.Subject != fixtureSubject || id.FromToken() {
		return nil
	}

	return []acl.Grant{{Resources: []string{"service:web"}, Permissions: []string{"write"}}}
}

// Provider grants reach a token through the snapshot taken at consent, and
// survive a refresh, which happens with no provider input at all.
func TestATokenReachesTheProviderGrantsOfItsSession(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{})
	evaluator.SetSource(fixtureSource{})

	s := newTestServer(t)
	s.cfg.GrantSnapshot = evaluator.SnapshotGrants

	tokens, clientID := mintTokensForIdentity(t, s)

	fromToken, err := s.Identify(tokens.AccessToken, s.resources.fallback)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if !evaluator.Can(fromToken, "write", "service:web") {
		t.Fatalf("a token is denied the provider grant its session holds: %+v", fromToken)
	}
	if evaluator.Can(fromToken, "write", "service:db") {
		t.Errorf("a token reaches past the provider grant: %+v", fromToken)
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokens.RefreshToken},
		"client_id":     {clientID},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.HandleToken(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d: %s", rec.Code, rec.Body.String())
	}

	var refreshed tokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&refreshed); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}

	fromRefresh, err := s.Identify(refreshed.AccessToken, s.resources.fallback)
	if err != nil {
		t.Fatalf("Identify refreshed: %v", err)
	}
	if !evaluator.Can(fromRefresh, "write", "service:web") {
		t.Errorf("a refreshed token lost the provider grant: %+v", fromRefresh)
	}
}

// Without a snapshot a token holds no provider grant, whatever its Raw says:
// only a token this server signed can carry one.
func TestATokenWithoutASnapshotHoldsNoProviderGrant(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{})
	evaluator.SetSource(fixtureSource{})

	s := newTestServer(t)

	fromToken, err := s.Identify(mintTokenForIdentity(t, s), s.resources.fallback)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if evaluator.Can(fromToken, "write", "service:web") {
		t.Errorf("a token minted without a snapshot reached the provider grant: %+v", fromToken)
	}
}
