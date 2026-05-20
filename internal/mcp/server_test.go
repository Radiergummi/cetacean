package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp/oauth"
)

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
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`)
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
