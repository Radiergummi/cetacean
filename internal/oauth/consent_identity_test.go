package oauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

// The identity a verified bearer token produces, which is what the auth
// middleware puts in the context for a token-authenticated request.
func tokenIdentity() *auth.Identity {
	return &auth.Identity{
		Subject:  fixtureSubject,
		Email:    fixtureEmail,
		Provider: ProviderName,
	}
}

func providerIdentity() *auth.Identity {
	return &auth.Identity{
		Subject:  fixtureSubject,
		Email:    fixtureEmail,
		Provider: "oidc",
	}
}

// consentGET drives a GET /oauth/authorize under identity, returning the
// response and the client it registered.
func consentGET(
	t *testing.T,
	s *Server,
	identity *auth.Identity,
	redirectURI string,
) *httptest.ResponseRecorder {
	t.Helper()

	target := authorizeURL(
		registeredClient(t, s, []string{redirectURI}),
		redirectURI,
		computeS256Challenge("verifier-padded-to-the-RFC-7636-minimum-length"),
		"state",
		s.resources.fallback,
	)

	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), identity))

	rec := httptest.NewRecorder()
	s.HandleAuthorize(rec, req)

	return rec
}

// A token this server issued must not found another grant. Consent sits outside
// the middleware's exempt set so it runs under the upstream provider, and once a
// bearer credential satisfies that middleware a leaked access token could
// otherwise mint a fresh grant with no human in the loop.
//
// 403, not 401: the request was authenticated, and RFC 9110 §15.5.2's promise is
// that repeating a 401 with a better credential of the same kind may work — which
// is not true here, whatever token the caller sends.
func TestConsentRefusesAnIdentityFromOurOwnToken(t *testing.T) {
	s := newTestServer(t)

	rec := consentGET(t, s, tokenIdentity(), "http://localhost:8612/cb")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("refusal redirected to %q", loc)
	}
}

// Every 401 must carry a challenge (RFC 9110 §15.5.2), and consent is reached
// through the auth middleware, so an unauthenticated one has to say where a
// credential comes from.
func TestConsentWithoutAnIdentityChallenges(t *testing.T) {
	s := newTestServer(t)

	const redirectURI = "http://localhost:8616/cb"
	target := authorizeURL(
		registeredClient(t, s, []string{redirectURI}),
		redirectURI,
		computeS256Challenge("verifier-padded-to-the-RFC-7636-minimum-length"),
		"state",
		s.resources.fallback,
	)

	rec := httptest.NewRecorder()
	s.HandleAuthorize(rec, httptest.NewRequest(http.MethodGet, target, nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}
	if challenge := rec.Header().Get("WWW-Authenticate"); challenge == "" {
		t.Error("a 401 with no WWW-Authenticate challenge")
	}
}

// The POST is checked on its own, not merely protected by the GET above: an
// attacker who obtained a consent form some other way must still not be able to
// approve it with a token, and a later change to the GET must not silently open
// this path.
func TestConsentPOSTRefusesAnIdentityFromOurOwnToken(t *testing.T) {
	s := newTestServer(t)

	// A legitimate GET yields the form and the CSRF nonce, so the POST is refused
	// for the credential it carries rather than for a missing token.
	page := consentGET(t, s, providerIdentity(), "http://localhost:8613/cb")
	if page.Code != http.StatusOK {
		t.Fatalf("GET consent: %d: %s", page.Code, page.Body.String())
	}

	form := consentForm(page.Body.String(), "approve", nil)
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
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), tokenIdentity()))

	rec := httptest.NewRecorder()
	s.HandleAuthorize(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("an approval redirected to %q; a code may have been issued", loc)
	}
}

// The same request under an upstream provider's identity still reaches consent,
// so the refusal is about where the credential came from and nothing else.
func TestConsentAcceptsAnIdentityFromTheProvider(t *testing.T) {
	s := newTestServer(t)

	rec := consentGET(t, s, providerIdentity(), "http://localhost:8614/cb")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the consent page: %s", rec.Code, rec.Body.String())
	}
}
