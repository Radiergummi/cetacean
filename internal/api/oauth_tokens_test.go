package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/oauth"
)

const (
	tokenTestIssuer = "https://cetacean.test"
	tokenTestRoot   = "cetacean-test-root-32-bytes-ok!!"
)

// tokenTestServer is an authorization server wired as main.go wires it, serving
// the deployment root and one resource beneath it.
func tokenTestServer() *oauth.Server {
	return oauth.NewServer(oauth.ServerConfig{
		Issuer: tokenTestIssuer,
		Resources: []oauth.Resource{
			{Path: "", Realm: "cetacean"},
			{Path: "/mcp", Realm: "cetacean-mcp"},
		},
		OAuth:      config.DefaultOAuthConfig(),
		SigningKey: []byte(tokenTestRoot),
	})
}

// tokenFor mints a real ES256 token for resourcePath, carrying identity.
func tokenFor(t *testing.T, resourcePath string, identity *auth.Identity) string {
	t.Helper()

	issuer, err := oauth.NewTokenIssuer([]byte(tokenTestRoot), tokenTestIssuer)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	token, err := issuer.IssueAccessToken(oauth.AccessTokenClaims{
		Subject:     identity.Subject,
		Email:       identity.Email,
		DisplayName: identity.DisplayName,
		Groups:      identity.Groups,
		ClientID:    "test-client",
	}, tokenTestIssuer+resourcePath, time.Hour)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	return token
}

// tokenRouter serves the real router with the API offered as a resource and a
// provider that authenticates nobody, so only a token can get in.
func tokenRouter(t *testing.T, opts ...testHandlersOption) http.Handler {
	t.Helper()

	srv := tokenTestServer()

	return newTestRouterWithConfig(t, []routerOption{
		func(cfg *RouterConfig) {
			cfg.AuthProvider = &refusingProvider{}
			cfg.OAuthRoutes = srv.RegisterRoutes
			cfg.APITokens = auth.APITokens{
				Verifier: srv,
				Resource: srv.ResourceIdentifier(""),
			}
		},
	}, opts...)
}

// refusingProvider establishes nothing, so any request that reaches it is one
// the token path declined to answer.
type refusingProvider struct{}

func (p *refusingProvider) Authenticate(
	_ http.ResponseWriter,
	_ *http.Request,
) (*auth.Identity, error) {
	return nil, &auth.AuthError{Msg: "no ambient credential", WWWAuthenticate: "Bearer"}
}

func (p *refusingProvider) RegisterRoutes(_ *http.ServeMux) {}

func getWithToken(t *testing.T, router http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Accept", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)

	return w
}

func TestATokenForTheAPIAuthenticatesARequest(t *testing.T) {
	router := tokenRouter(t, withCache(cache.New(nil)))
	token := tokenFor(t, "", &auth.Identity{Subject: "alice", Email: "alice@example.com"})

	if w := getWithToken(t, router, "/nodes", token); w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// One path lying under another does not make one audience contain the other. A
// token the user granted an agent for the MCP transport must not open the REST
// write surface, and the refusal must name the API's own metadata document —
// sent to the wrong one, a client would fetch a token that fails the same way.
func TestATokenForAnotherResourceIsRefusedAndPointedAtTheRightDocument(t *testing.T) {
	router := tokenRouter(t, withCache(cache.New(nil)))
	token := tokenFor(t, "/mcp", &auth.Identity{Subject: "alice", Email: "alice@example.com"})

	w := getWithToken(t, router, "/nodes", token)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
	}

	challenge := w.Header().Get("WWW-Authenticate")
	want := `resource_metadata="` + tokenTestIssuer + `/.well-known/oauth-protected-resource"`
	if !strings.Contains(challenge, want) {
		t.Errorf("WWW-Authenticate = %q, want substring %q", challenge, want)
	}
	if strings.Contains(challenge, "oauth-protected-resource/mcp") {
		t.Errorf("the refusal names the resource the token was for, not the API: %q", challenge)
	}
}

// The grant docs/authorization.md documents is keyed on email, and the subject
// here is not an address. The same person over a token must receive the same
// Allow as over a session, or the ACL silently depends on the credential.
func TestATokenReceivesTheSameAllowAsASession(t *testing.T) {
	const subject = "a3f1c8e2-7b04-4d19-9e55-2c6f0b8a41d7"

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-web",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:*"},
		Audience:    []string{"user:*@example.com"},
		Permissions: []string{"write"},
	}}})

	identity := &auth.Identity{
		Subject: subject,
		Email:   "alice@example.com",
	}

	opts := []testHandlersOption{
		withCache(c),
		withACL(evaluator),
		withOpsLevel(config.OpsImpactful),
	}

	// The session arm runs the same router with a provider that establishes the
	// identity directly, so the only difference between the two is the credential.
	sessionRouter := newTestRouterWithConfig(t, []routerOption{
		func(cfg *RouterConfig) { cfg.AuthProvider = &fixedProvider{identity: identity} },
	}, opts...)

	overSession := getWithToken(t, sessionRouter, "/services/svc-web", "")
	if overSession.Code != http.StatusOK {
		t.Fatalf("session: status = %d, want 200: %s", overSession.Code, overSession.Body.String())
	}

	overToken := getWithToken(
		t,
		tokenRouter(t, opts...),
		"/services/svc-web",
		tokenFor(t, "", identity),
	)
	if overToken.Code != http.StatusOK {
		t.Fatalf("token: status = %d, want 200: %s", overToken.Code, overToken.Body.String())
	}

	session, viaToken := overSession.Header().Get("Allow"), overToken.Header().Get("Allow")
	if session == "" {
		t.Fatal("the session arm reported no Allow; the fixture proves nothing")
	}
	if !strings.Contains(session, "DELETE") {
		t.Fatalf("session Allow = %q, want the grant to reach a write method", session)
	}
	if viaToken != session {
		t.Errorf("Allow over a token = %q, over a session = %q", viaToken, session)
	}
}

// fixedProvider establishes one identity, standing in for a valid session.
type fixedProvider struct{ identity *auth.Identity }

func (p *fixedProvider) Authenticate(
	_ http.ResponseWriter,
	_ *http.Request,
) (*auth.Identity, error) {
	return p.identity, nil
}

func (p *fixedProvider) RegisterRoutes(_ *http.ServeMux) {}
