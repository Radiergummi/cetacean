package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp/oauth"
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

	oauthSrv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://cetacean.example.com",
		BasePath:    "",
		MCPResource: "https://cetacean.example.com/mcp",
		MCP:         cfg,
		SigningKey:  []byte("test-secret-32-bytes-long-padding"),
	})

	srv, err := New(c, Options{
		Config: cfg,
		OAuth:  oauthSrv,
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
	oauthSrv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://cetacean.example.com",
		BasePath:    "",
		MCPResource: "https://cetacean.example.com/mcp",
		MCP:         cfg,
		SigningKey:  key,
	})

	issuer := &oauth.TokenIssuer{
		SigningKey: key,
		Issuer:     "https://cetacean.example.com",
		Audience:   "https://cetacean.example.com/mcp",
	}
	token, err := issuer.IssueAccessToken(oauth.AccessTokenClaims{
		Subject: "user@example.com",
		Groups:  []string{"ops"},
	}, cfg.AccessTokenTTL)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	srv, err := New(c, Options{
		Config: cfg,
		OAuth:  oauthSrv,
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

	oauthSrv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://cetacean.example.com",
		BasePath:    "",
		MCPResource: "https://cetacean.example.com/mcp",
		MCP:         cfg,
		SigningKey:  []byte("test-secret-32-bytes-long-padding"),
	})

	provider := &fakeAuthProvider{id: &auth.Identity{
		Subject:  "spiffe://example.org/agent/runner",
		Groups:   []string{"ops"},
		Provider: "cert",
	}}

	srv, err := New(c, Options{
		Config:       cfg,
		OAuth:        oauthSrv,
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

	oauthSrv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://cetacean.example.com",
		BasePath:    "",
		MCPResource: "https://cetacean.example.com/mcp",
		MCP:         cfg,
		SigningKey:  []byte("test-secret-32-bytes-long-padding"),
	})

	provider := &fakeAuthProvider{err: errors.New("no client certificate")}

	srv, err := New(c, Options{
		Config:       cfg,
		OAuth:        oauthSrv,
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

	oauthSrv := oauth.NewServer(oauth.ServerConfig{
		Issuer:      "https://cetacean.example.com",
		BasePath:    "",
		MCPResource: "https://cetacean.example.com/mcp",
		MCP:         cfg,
		SigningKey:  []byte("test-secret-32-bytes-long-padding"),
	})

	// Provider would succeed, but the active mode (oidc) is NOT in AuthBypass.
	provider := &fakeAuthProvider{id: &auth.Identity{Subject: "u", Provider: "oidc"}}

	srv, err := New(c, Options{
		Config:       cfg,
		OAuth:        oauthSrv,
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
