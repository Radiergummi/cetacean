package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/oauth"
)

// fakeAuthProvider returns a fixed identity (or error) without touching the
// ResponseWriter. Mirrors how CertProvider behaves on the success path.
type fakeAuthProvider struct {
	id  *auth.Identity
	err error
}

func (p *fakeAuthProvider) Authenticate(
	_ http.ResponseWriter,
	_ *http.Request,
) (*auth.Identity, error) {
	return p.id, p.err
}

func (p *fakeAuthProvider) RegisterRoutes(_ *http.ServeMux) {}

// The issuer and resource the test authorization server advertises. A token
// only verifies when its issuer and audience match these exactly, which is why
// they are named rather than spelled at each site.
const (
	testIssuer   = "https://cetacean.example.com"
	testResource = testIssuer + MountPath
)

// oauthServerFor builds an authorization server the way main.go does, sharing
// the root key so a token minted against it verifies. The tests want the real
// verifier, not a stand-in.
func oauthServerFor(key []byte) *oauth.Server {
	return oauth.NewServer(oauth.ServerConfig{
		Issuer:   testIssuer,
		BasePath: "",
		Resources: []oauth.Resource{
			{Path: "", Realm: "cetacean"},
			{Path: MountPath, Realm: "cetacean-mcp"},
		},
		OAuth:      config.DefaultOAuthConfig(),
		SigningKey: key,
	})
}

// tokenFor mints a bearer token the server from oauthServerFor will accept.
func tokenFor(t *testing.T, key []byte, claims oauth.AccessTokenClaims) string {
	t.Helper()

	issuer, err := oauth.NewTokenIssuer(key, testIssuer)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	token, err := issuer.IssueAccessToken(
		claims,
		testResource,
		config.DefaultOAuthConfig().AccessTokenTTL,
	)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	return token
}

func TestNew(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	srv, err := New(c, Options{
		Config:         cfg,
		GlobalOpsLevel: config.OpsReadOnly,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv == nil {
		t.Fatal("server is nil")
	}
	if srv.Handler() == nil {
		t.Fatal("Handler() returned nil")
	}
}

func TestNewRequiresCache(t *testing.T) {
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	if _, err := New(nil, Options{Config: cfg}); err == nil {
		t.Fatal("expected error when cache is nil")
	}
}

func TestHandlerEmits401WithoutBearerWhenOAuthConfigured(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	oauthSrv := oauthServerFor([]byte("test-secret-32-bytes-long-padding"))

	srv, err := New(c, Options{
		Config:   cfg,
		OAuth:    oauthSrv,
		Resource: testResource,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	www := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(www, "Bearer") {
		t.Errorf("WWW-Authenticate = %q, want Bearer challenge", www)
	}
	if !strings.Contains(www, "resource_metadata=") {
		t.Errorf("WWW-Authenticate missing resource_metadata: %q", www)
	}
}

func TestHandlerAcceptsValidBearer(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	key := []byte("test-secret-32-bytes-long-padding")
	oauthSrv := oauthServerFor(key)

	token := tokenFor(t, key, oauth.AccessTokenClaims{
		Subject:  "user@example.com",
		Groups:   []string{"ops"},
		ClientID: "test-client",
	})

	srv, err := New(c, Options{
		Config:   cfg,
		OAuth:    oauthSrv,
		Resource: testResource,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	// Initialize request — mcp-go requires this as the first call on a session.
	body := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)

	srv.Handler().ServeHTTP(rec, req)

	// The token must pass the bearer middleware; whatever mcp-go decides about
	// the body is fine as long as we don't see the 401 produced by the
	// middleware.
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("valid bearer rejected: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerAuthBypassUsesUpstreamIdentity(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	cfg.AuthBypass = []string{"cert"}

	oauthSrv := oauthServerFor([]byte("test-secret-32-bytes-long-padding"))

	provider := &fakeAuthProvider{id: &auth.Identity{
		Subject:  "spiffe://example.org/agent/runner",
		Groups:   []string{"ops"},
		Provider: "cert",
	}}

	srv, err := New(c, Options{
		Config:       cfg,
		OAuth:        oauthSrv,
		Resource:     testResource,
		AuthMode:     "cert",
		AuthProvider: provider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	body := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	// Deliberately no Authorization header.

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("bypass identity rejected: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerAuthBypassFallsBackWhenUpstreamFails(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	cfg.AuthBypass = []string{"cert"}

	oauthSrv := oauthServerFor([]byte("test-secret-32-bytes-long-padding"))

	provider := &fakeAuthProvider{err: errors.New("no client certificate")}

	srv, err := New(c, Options{
		Config:       cfg,
		OAuth:        oauthSrv,
		Resource:     testResource,
		AuthMode:     "cert",
		AuthProvider: provider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	srv.Handler().ServeHTTP(rec, req)

	// No upstream identity and no bearer token → 401 from the OAuth path.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when both bypass and bearer fail", rec.Code)
	}
}

func TestCloseIsIdempotentUnderConcurrency(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	srv, err := New(c, Options{Config: cfg, GlobalOpsLevel: config.OpsReadOnly})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Fire many concurrent Close calls; the underlying cache cancel must run
	// exactly once. With sync.Once this is race-free; the race detector
	// (`go test -race`) catches the prior implementation.
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			srv.Close()
		}()
	}
	wg.Wait()
}

func TestHandlerAuthBypassIgnoredWhenModeNotListed(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	cfg.AuthBypass = []string{"cert"} // listed mode

	oauthSrv := oauthServerFor([]byte("test-secret-32-bytes-long-padding"))

	// Provider would succeed, but the active mode (oidc) is NOT in AuthBypass.
	provider := &fakeAuthProvider{id: &auth.Identity{Subject: "u", Provider: "oidc"}}

	srv, err := New(c, Options{
		Config:       cfg,
		OAuth:        oauthSrv,
		Resource:     testResource,
		AuthMode:     "oidc",
		AuthProvider: provider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	// No bearer; bypass mismatch must NOT let the request through.

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when active mode is not in AuthBypass", rec.Code)
	}
}

// The identity a bearer token yields is the whole input to the ACL, so every
// field of it is load-bearing — including the ones left empty, which is why the
// comparison is exact rather than field-by-field.
func TestBearerAuthBuildsTheIdentityFromClaims(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	key := []byte("test-secret-32-bytes-long-padding")
	oauthSrv := oauthServerFor(key)

	token := tokenFor(t, key, oauth.AccessTokenClaims{
		Subject:  "user@example.com",
		Groups:   []string{"ops"},
		ClientID: "test-client",
	})

	srv, err := New(c, Options{Config: cfg, OAuth: oauthSrv, Resource: testResource})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var got *auth.Identity
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = auth.IdentityFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	srv.bearerAuth(next).ServeHTTP(httptest.NewRecorder(), req)

	want := &auth.Identity{
		Subject:  "user@example.com",
		Groups:   []string{"ops"},
		Provider: oauth.ProviderName,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("identity = %+v, want %+v", got, want)
	}
}

// Without an authorization server there is no bearer middleware, which is only
// safe because auth mode "none" is the only configuration that reaches here.
// Many tool tests depend on it incidentally; this one says so.
// Unguarded is reachable only under an auth mode that establishes no identity
// to begin with. Every other route to it is a refusal, below.
func TestHandlerWithoutOAuthServesUnguardedUnderNone(t *testing.T) {
	for _, mode := range []string{"", "none"} {
		t.Run("mode="+mode, func(t *testing.T) {
			cfg := config.DefaultMCPConfig()
			cfg.Enabled = true

			srv, err := New(cache.New(nil), Options{Config: cfg, AuthMode: mode})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")

			srv.Handler().ServeHTTP(rec, req)

			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("status = 401 with no OAuth server configured; want unguarded")
			}
		})
	}
}

// The construction that would have served the cluster's write surface to
// anyone: a mode that establishes an identity, nothing to verify a token
// against, and no bypass to fall back on. main's config validation refuses it
// too; this is the same refusal in the package that owns the endpoint.
func TestNewRefusesAConfigurationThatWouldServeUnguarded(t *testing.T) {
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	_, err := New(cache.New(nil), Options{
		Config:       cfg,
		AuthMode:     "oidc",
		AuthProvider: &fakeAuthProvider{id: &auth.Identity{Subject: "alice"}},
	})
	if err == nil {
		t.Fatal("a mode with neither a verifier nor a bypass was accepted")
	}
	if !strings.Contains(err.Error(), "unauthenticated") {
		t.Errorf("error does not say what is at stake: %v", err)
	}
}

// A bypass names a mode; it still needs the provider that answers for it.
// Without one the upstream guard would have nothing to call.
func TestNewRefusesABypassWithNoProvider(t *testing.T) {
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	cfg.AuthBypass = []string{"cert"}

	_, err := New(cache.New(nil), Options{Config: cfg, AuthMode: "cert"})
	if err == nil {
		t.Fatal("a bypass without a provider was accepted")
	}
}

// The mTLS deployment: no authorization server at all, because these clients
// cannot drive a browser consent screen. The upstream provider is the guard.
func TestHandlerWithoutOAuthAuthenticatesABypassedMode(t *testing.T) {
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	cfg.AuthBypass = []string{"cert"}

	provider := &fakeAuthProvider{id: &auth.Identity{
		Subject:  "spiffe://example.org/agent/runner",
		Provider: "cert",
	}}

	srv, err := New(cache.New(nil), Options{
		Config:       cfg,
		AuthMode:     "cert",
		AuthProvider: provider,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	body := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("bypass identity rejected: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// With no authorization server there is no second chance: whatever the upstream
// provider refuses is refused, including the identity it declines to establish
// while writing a redirect nobody reads. The refusal is 403, not 401: no
// challenge can ask for the credential these modes read, which is the same
// ruling the API endpoints answer under.
func TestHandlerWithoutOAuthRefusesWhatUpstreamRefuses(t *testing.T) {
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	cfg.AuthBypass = []string{"cert"}

	for name, provider := range map[string]*fakeAuthProvider{
		"an error":           {err: errors.New("no client certificate")},
		"no identity at all": {},
	} {
		t.Run(name, func(t *testing.T) {
			srv, err := New(cache.New(nil), Options{
				Config:       cfg,
				AuthMode:     "cert",
				AuthProvider: provider,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")

			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}

			if challenge := rec.Header().Get("WWW-Authenticate"); challenge != "" {
				t.Fatalf("WWW-Authenticate = %q, want none on a 403", challenge)
			}
		})
	}
}

// A host listing several MCP servers shows the title, falls back to the
// programmatic name when there is none, and offers the website as the "what is
// this" link beside it. Cetacean declared only a name and a description, so it
// rendered as "cetacean" with nowhere to go.
func TestServerDiscoverCarriesTheFullIdentity(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) {
		o.IconBaseURL = "https://swarm.example.com"
	})

	_, envelope := mcpModern(t, srv.Handler(), 1, "server/discover", `{}`)
	if envelope.Error != nil {
		t.Fatalf("server/discover: %+v", envelope.Error)
	}

	// server/discover carries the implementation block in _meta under the
	// spec's reverse-DNS key, not as the top-level serverInfo of the
	// initialize handshake it replaced.
	type implementation struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		WebsiteURL  string `json:"websiteUrl"`
		Icons       []struct {
			Src      string `json:"src"`
			MIMEType string `json:"mimeType"`
		} `json:"icons"`
	}

	var discovered struct {
		Meta struct {
			ServerInfo implementation `json:"io.modelcontextprotocol/serverInfo"`
		} `json:"_meta"`
	}

	if err := json.Unmarshal(envelope.Result, &discovered); err != nil {
		t.Fatalf("decode server/discover: %v (raw %s)", err, envelope.Result)
	}

	info := discovered.Meta.ServerInfo

	if info.Name != "cetacean" {
		t.Errorf("name = %q, want cetacean", info.Name)
	}

	if info.Title != mcpTitle {
		t.Errorf("title = %q, want %q", info.Title, mcpTitle)
	}

	if info.Description != mcpDescription {
		t.Errorf("description = %q, want %q", info.Description, mcpDescription)
	}

	if info.WebsiteURL != mcpWebsiteURL {
		t.Errorf("websiteUrl = %q, want %q", info.WebsiteURL, mcpWebsiteURL)
	}

	if len(info.Icons) != 1 {
		t.Fatalf("icons = %+v, want exactly one", info.Icons)
	}

	// An icon src must be an absolute https:// or data: URI per the spec — a
	// relative path is what a client cannot resolve, having no base to
	// resolve it against.
	want := "https://swarm.example.com/assets/mcp-icons/server/cetacean.svg"
	if info.Icons[0].Src != want {
		t.Errorf("icon src = %q, want %q", info.Icons[0].Src, want)
	}
}

// Without a canonical external base there is no absolute URI to name, so the
// server says nothing about icons rather than publishing one a client cannot
// fetch — the same rule the per-tool icons follow.
func TestServerDiscoverOmitsIconsWithoutABaseURL(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	_, envelope := mcpModern(t, srv.Handler(), 1, "server/discover", `{}`)
	if envelope.Error != nil {
		t.Fatalf("server/discover: %+v", envelope.Error)
	}

	if bytes.Contains(envelope.Result, []byte(`"icons"`)) {
		t.Errorf("icons advertised with no base URL to build them from: %s", envelope.Result)
	}
}

// The other half of the separation: a token minted for the deployment root — the
// credential an ordinary API client holds — must not reach this transport, and
// the refusal must send the client to this transport's own metadata document.
//
// The audiences differ by one path segment and one is a prefix of the other,
// which is exactly the pair a containment reading would conflate.
func TestHandlerRefusesATokenForAnotherResource(t *testing.T) {
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	key := []byte("test-secret-32-bytes-long-padding")
	oauthSrv := oauthServerFor(key)

	issuer, err := oauth.NewTokenIssuer(key, testIssuer)
	if err != nil {
		t.Fatalf("NewTokenIssuer: %v", err)
	}

	// testIssuer alone is the deployment root, where testResource is /mcp beneath it.
	token, err := issuer.IssueAccessToken(oauth.AccessTokenClaims{
		Subject:  "user@example.com",
		ClientID: "test-client",
	}, testIssuer, config.DefaultOAuthConfig().AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	srv, err := New(cache.New(nil), Options{Config: cfg, OAuth: oauthSrv, Resource: testResource})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(srv.Close)

	req := httptest.NewRequest(http.MethodPost, MountPath, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
	}

	challenge := rec.Header().Get("WWW-Authenticate")
	want := `resource_metadata="` + testIssuer +
		`/.well-known/oauth-protected-resource` + MountPath + `"`
	if !strings.Contains(challenge, want) {
		t.Errorf("WWW-Authenticate = %q, want substring %q", challenge, want)
	}
}
