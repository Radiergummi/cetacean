package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

type stubVerifier struct {
	identity *Identity
	err      error
	asked    bool
}

func (v *stubVerifier) Identify(_, _ string) (*Identity, error) {
	v.asked = true

	return v.identity, v.err
}

// Mirrors the real verifier, which omits the error parameter for a request that
// carried no credential (RFC 6750 §3.1). A stub that always emitted it would let
// that conformance regress unnoticed.
func (v *stubVerifier) UnauthorizedHeader(resource, errorCode string) string {
	challenge := `Bearer realm="cetacean", resource_metadata="` + resource +
		`/.well-known/oauth-protected-resource"`
	if errorCode == "" {
		return challenge
	}

	return challenge + `, error="` + errorCode + `"`
}

// cookieProvider stands in for a provider that authenticates from an ambient
// credential and redirects a browser that has none, which is what oidc mode does.
type cookieProvider struct {
	identity *Identity
	asked    bool
}

func (p *cookieProvider) Authenticate(
	w http.ResponseWriter,
	r *http.Request,
) (*Identity, error) {
	p.asked = true
	if p.identity != nil {
		return p.identity, nil
	}

	http.Redirect(w, r, "/auth/login", http.StatusFound)

	return nil, nil
}

func (p *cookieProvider) RegisterRoutes(_ *http.ServeMux) {}

func bearerRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}

	return r
}

// serve runs the middleware and reports the identity the inner handler saw.
func serve(
	provider Provider,
	tokens APITokens,
	r *http.Request,
) (*httptest.ResponseRecorder, *Identity) {
	var seen *Identity
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	Middleware(provider, tokens)(inner).ServeHTTP(w, r)

	return w, seen
}

// The two dialects share one header, so the verdict decides which credential the
// request is judged by. A token of ours that failed must never reach the
// provider — judged there it would be measured against weaker evidence, or
// answered with a login page a non-browser client cannot use.
func TestTheVerifiersVerdictDecidesWhoJudgesTheRequest(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc6750/challenge-on-missing-or-invalid-credentials",
		"oauth/rfc6750/invalid-token-is-401",
	)

	const resource = "https://cetacean.test"

	fromToken := &Identity{Subject: "alice", Provider: "oauth"}
	fromProvider := &Identity{Subject: "bob", Provider: "oidc"}

	cases := []struct {
		name           string
		verifierErr    error
		wantStatus     int
		wantProvider   bool
		wantIdentity   *Identity
		wantChallenged bool
	}{
		{
			name:         "a token that verifies authenticates the request itself",
			wantStatus:   http.StatusOK,
			wantProvider: false,
			wantIdentity: fromToken,
		},
		{
			name:         "somebody else's token falls through to the provider",
			verifierErr:  fmt.Errorf("%w: another issuer", ErrForeignToken),
			wantStatus:   http.StatusOK,
			wantProvider: true,
			wantIdentity: fromProvider,
		},
		{
			name:           "ours, refused for cause, is final",
			verifierErr:    errors.New("expired"),
			wantStatus:     http.StatusUnauthorized,
			wantProvider:   false,
			wantChallenged: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			verifier := &stubVerifier{identity: fromToken, err: c.verifierErr}
			provider := &cookieProvider{identity: fromProvider}

			w, seen := serve(
				provider,
				APITokens{Verifier: verifier, Resource: resource},
				bearerRequest("a-token"),
			)

			if !verifier.asked {
				t.Error("the verifier was not consulted")
			}
			if provider.asked != c.wantProvider {
				t.Errorf("provider consulted = %v, want %v", provider.asked, c.wantProvider)
			}
			if w.Code != c.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, c.wantStatus)
			}
			if seen != c.wantIdentity {
				t.Errorf("identity = %+v, want %+v", seen, c.wantIdentity)
			}

			challenge := w.Header().Get("WWW-Authenticate")
			if c.wantChallenged {
				// The refusal has to point at the resource's own metadata, or a
				// client cannot find out where to get a token that would work.
				if !strings.Contains(challenge, resource) {
					t.Errorf("WWW-Authenticate = %q, want it to name %q", challenge, resource)
				}
				if loc := w.Header().Get("Location"); loc != "" {
					t.Errorf("refusal redirected to %q; a token client cannot follow it", loc)
				}
			} else if challenge != "" {
				t.Errorf("WWW-Authenticate = %q on a request that was not refused", challenge)
			}
		})
	}
}

// An explicit credential outranks the ambient one. A browser carrying both would
// otherwise be judged by whichever the middleware happened to read first, and a
// client that deliberately sent a token would find it silently ignored.
func TestATokenOutranksASession(t *testing.T) {
	session := &Identity{Subject: "from-cookie", Provider: "oidc"}
	token := &Identity{Subject: "from-token", Provider: "oauth"}

	provider := &cookieProvider{identity: session}
	_, seen := serve(
		provider,
		APITokens{Verifier: &stubVerifier{identity: token}, Resource: "https://cetacean.test"},
		bearerRequest("a-token"),
	)

	if seen != token {
		t.Errorf("identity = %+v, want the token's %+v", seen, token)
	}
	if provider.asked {
		t.Error("the provider was consulted even though the token verified")
	}
}

// The paths that must behave exactly as they did before tokens existed: a
// deployment running no authorization server, and a request that carries no
// bearer credential at all.
func TestWithoutABearerTokenNothingChanges(t *testing.T) {
	session := &Identity{Subject: "from-cookie", Provider: "oidc"}

	t.Run("no verifier configured", func(t *testing.T) {
		provider := &cookieProvider{identity: session}

		if _, seen := serve(provider, APITokens{}, bearerRequest("a-token")); seen != session {
			t.Errorf("identity = %+v, want %+v", seen, session)
		}
		if !provider.asked {
			t.Error("the provider was not consulted")
		}
	})

	t.Run("no Authorization header", func(t *testing.T) {
		verifier := &stubVerifier{identity: &Identity{Subject: "unused"}}
		provider := &cookieProvider{identity: session}

		_, seen := serve(
			provider,
			APITokens{Verifier: verifier, Resource: "https://cetacean.test"},
			bearerRequest(""),
		)

		if verifier.asked {
			t.Error("the verifier was consulted without a bearer token to verify")
		}
		if seen != session {
			t.Errorf("identity = %+v, want %+v", seen, session)
		}
	})
}

// RFC 9728's whole point is that a client can call a protected resource cold,
// read the 401, and follow resource_metadata to find out where a token comes
// from. That only works if the challenge is there when no credential was sent —
// which is the case the provider answers, not the verifier.
func TestAColdCallIsToldWhereATokenComesFrom(t *testing.T) {
	spec.Satisfies(t,
		"oauth/rfc6750/challenge-on-missing-or-invalid-credentials",
		"oauth/rfc6750/no-error-code-without-a-credential",
	)

	const resource = "https://cetacean.test"

	// A provider that refuses with its own scheme, as cert and oidc modes do.
	provider := &schemeProvider{scheme: "mutual-tls"}
	tokens := APITokens{Verifier: &stubVerifier{}, Resource: resource}

	w, _ := serve(provider, tokens, bearerRequest(""))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}

	challenges := w.Result().Header.Values("WWW-Authenticate")

	// The provider's own scheme survives: an mTLS client still learns it may
	// present a certificate instead.
	var named, kept bool
	for _, c := range challenges {
		if strings.Contains(c, "mutual-tls") {
			kept = true
		}
	}
	if !kept {
		t.Errorf("the provider's own challenge was dropped: %q", challenges)
	}

	for _, c := range challenges {
		if strings.Contains(c, "resource_metadata=") && strings.Contains(c, resource) {
			named = true
		}
		// RFC 6750 §3.1: nothing was sent, so nothing is reported as invalid.
		if strings.Contains(c, "error=") {
			t.Errorf("challenge reports an error for a missing credential: %q", c)
		}
	}
	if !named {
		t.Errorf("no challenge names the resource's metadata: %q", challenges)
	}
}

// A provider with no challenge of its own refuses with 403, because a client
// cannot act on it. Offering the resource's own challenge is what makes the
// refusal actionable, so it becomes a 401 — the same rule, not an exception.
func TestAChallengelessProviderStillPointsAtTheResource(t *testing.T) {
	const resource = "https://cetacean.test"

	w, _ := serve(
		&failProvider{},
		APITokens{Verifier: &stubVerifier{}, Resource: resource},
		bearerRequest(""),
	)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 once a challenge names where a token comes from", w.Code)
	}

	var named bool
	for _, c := range w.Result().Header.Values("WWW-Authenticate") {
		if strings.Contains(c, "resource_metadata=") && strings.Contains(c, resource) {
			named = true
		}
	}
	if !named {
		t.Error("401 with no challenge naming the resource's metadata")
	}
}

// The authorization endpoint is not a protected resource: it refuses a token
// this server issued, so inviting a client to mint one sends it in a circle.
func TestTheAuthorizationEndpointAdvertisesNoToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/oauth/authorize", nil)
	w, _ := serve(
		&failProvider{},
		APITokens{Verifier: &stubVerifier{}, Resource: "https://cetacean.test"},
		r,
	)

	for _, c := range w.Result().Header.Values("WWW-Authenticate") {
		if strings.Contains(c, "resource_metadata=") {
			t.Errorf("/oauth/authorize advertised a token endpoint: %q", c)
		}
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 where no challenge is offered", w.Code)
	}
}

// A deployment with no authorization server has no metadata to advertise, so the
// provider's challenge stands alone exactly as it did before tokens existed.
func TestAColdCallWithoutAVerifierIsUnchanged(t *testing.T) {
	w, _ := serve(&schemeProvider{scheme: "mutual-tls"}, APITokens{}, bearerRequest(""))

	for _, c := range w.Result().Header.Values("WWW-Authenticate") {
		if strings.Contains(c, "resource_metadata=") {
			t.Errorf("advertised metadata with no authorization server: %q", c)
		}
	}
}

// schemeProvider refuses with a challenge of its own, the way cert mode offers
// mutual-tls and oidc offers Bearer.
type schemeProvider struct{ scheme string }

func (p *schemeProvider) Authenticate(
	_ http.ResponseWriter,
	_ *http.Request,
) (*Identity, error) {
	return nil, &AuthError{Msg: "no credential", WWWAuthenticate: p.scheme}
}

func (p *schemeProvider) RegisterRoutes(_ *http.ServeMux) {}

// The middleware puts one of two kinds of identity in the context, and what a
// caller may do depends on which: a token is a credential left on a device.
func TestAnIdentityKnowsWhetherATokenCarriedIt(t *testing.T) {
	token := &Identity{Subject: "alice", Provider: ProviderToken}
	if !token.FromToken() {
		t.Error("an identity from a token did not say so")
	}

	session := &Identity{Subject: "alice", Provider: "oidc"}
	if session.FromToken() {
		t.Error("an identity from the provider claimed a token carried it")
	}

	var absent *Identity
	if absent.FromToken() {
		t.Error("no identity at all claimed a token carried it")
	}
}
