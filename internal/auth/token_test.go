package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// errForeign stands in for "this token belongs to somebody else". The real
// classification is the verifier's, and is pinned where the sentinels live; here
// it only has to be distinguishable.
var errForeign = errors.New("another issuer")

type stubVerifier struct {
	identity *Identity
	err      error
	asked    bool
}

func (v *stubVerifier) Identify(_, _ string) (*Identity, error) {
	v.asked = true

	return v.identity, v.err
}

func (v *stubVerifier) Foreign(err error) bool { return errors.Is(err, errForeign) }

func (v *stubVerifier) UnauthorizedHeader(resource, errorCode string) string {
	return `Bearer realm="cetacean", resource_metadata="` + resource +
		`/.well-known/oauth-protected-resource", error="` + errorCode + `"`
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
			verifierErr:  errForeign,
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
			if c.verifierErr != nil {
				verifier.identity = nil
			}
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
		verifier := &stubVerifier{}
		provider := &cookieProvider{identity: session}

		if _, seen := serve(provider, APITokens{}, bearerRequest("a-token")); seen != session {
			t.Errorf("identity = %+v, want %+v", seen, session)
		}
		if verifier.asked {
			t.Error("a verifier that was never configured was consulted")
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
