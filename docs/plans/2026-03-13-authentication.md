# Authentication Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add pluggable authentication to cetacean with five modes (none, OIDC, Tailscale, cert, headers), selected at startup via config.

**Architecture:** Single auth middleware inserted between `securityHeaders` and `negotiate` in the existing chain. A `Provider` interface abstracts each mode. The provider extracts identity from the request and returns a unified `Identity` struct stored in context. OIDC uses signed ephemeral cookies for browser sessions and Bearer token validation for machines. See `docs/plans/2026-03-13-authentication-design.md` for full design.

**Tech Stack:** Go stdlib `net/http`, `crypto/hmac`, `crypto/tls`; new deps: `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, `tailscale.com/tsnet`, `tailscale.com/client/tailscale`

---

### Task 1: Identity type and context helpers

**Files:**
- Create: `internal/auth/identity.go`
- Create: `internal/auth/identity_test.go`

**Step 1: Write the failing test**

```go
// internal/auth/identity_test.go
package auth

import (
	"context"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	id := &Identity{
		Subject:     "user-123",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Groups:      []string{"admin"},
		Provider:    "test",
		Raw:         map[string]any{"custom": "value"},
	}

	ctx := ContextWithIdentity(context.Background(), id)
	got := IdentityFromContext(ctx)
	if got == nil {
		t.Fatal("expected identity in context, got nil")
	}
	if got.Subject != "user-123" {
		t.Errorf("subject=%q, want %q", got.Subject, "user-123")
	}
	if got.DisplayName != "Alice" {
		t.Errorf("display_name=%q, want %q", got.DisplayName, "Alice")
	}
}

func TestIdentityFromContext_Missing(t *testing.T) {
	got := IdentityFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestContext -v`
Expected: FAIL — package doesn't exist yet

**Step 3: Write minimal implementation**

```go
// internal/auth/identity.go
package auth

import "context"

type Identity struct {
	Subject     string         `json:"subject"`
	DisplayName string         `json:"displayName"`
	Email       string         `json:"email,omitempty"`
	Groups      []string       `json:"groups,omitempty"`
	Provider    string         `json:"provider"`
	Raw         map[string]any `json:"raw,omitempty"`
}

type ctxKey struct{}

func ContextWithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func IdentityFromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(ctxKey{}).(*Identity)
	return id
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestContext -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/identity.go internal/auth/identity_test.go
git commit -m "feat(auth): add Identity type and context helpers"
```

---

### Task 2: Provider interface and none provider

**Files:**
- Create: `internal/auth/provider.go`
- Create: `internal/auth/none.go`
- Create: `internal/auth/none_test.go`

**Step 1: Write the failing test**

```go
// internal/auth/none_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoneProvider_Authenticate(t *testing.T) {
	p := &NoneProvider{}
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == nil {
		t.Fatal("expected identity, got nil")
	}
	if id.Subject != "anonymous" {
		t.Errorf("subject=%q, want %q", id.Subject, "anonymous")
	}
	if id.Provider != "none" {
		t.Errorf("provider=%q, want %q", id.Provider, "none")
	}
}

func TestNoneProvider_RegisterRoutes(t *testing.T) {
	p := &NoneProvider{}
	mux := http.NewServeMux()
	p.RegisterRoutes(mux) // should not panic
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestNoneProvider -v`
Expected: FAIL — types not defined

**Step 3: Write minimal implementation**

```go
// internal/auth/provider.go
package auth

import "net/http"

// Provider authenticates incoming requests.
type Provider interface {
	// Authenticate extracts identity from the request.
	// Returns (nil, nil) if the provider handled the response itself (e.g., redirect).
	// Returns (nil, error) if authentication failed.
	Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error)

	// RegisterRoutes adds provider-specific routes (e.g., OIDC callback).
	RegisterRoutes(mux *http.ServeMux)
}
```

```go
// internal/auth/none.go
package auth

import "net/http"

var anonymousIdentity = &Identity{
	Subject:     "anonymous",
	DisplayName: "Anonymous",
	Provider:    "none",
}

type NoneProvider struct{}

func (p *NoneProvider) Authenticate(_ http.ResponseWriter, _ *http.Request) (*Identity, error) {
	return anonymousIdentity, nil
}

func (p *NoneProvider) RegisterRoutes(_ *http.ServeMux) {}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestNoneProvider -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/auth/provider.go internal/auth/none.go internal/auth/none_test.go
git commit -m "feat(auth): add Provider interface and NoneProvider"
```

---

### Task 3: Auth middleware with route exemptions

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

**Step 1: Write the failing tests**

```go
// internal/auth/middleware_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware_InjectsIdentity(t *testing.T) {
	provider := &NoneProvider{}
	middleware := Middleware(provider)

	var gotID *Identity
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = IdentityFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(inner)
	req := httptest.NewRequest("GET", "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if gotID == nil {
		t.Fatal("expected identity in context")
	}
	if gotID.Subject != "anonymous" {
		t.Errorf("subject=%q, want %q", gotID.Subject, "anonymous")
	}
}

func TestMiddleware_ExemptRoutes(t *testing.T) {
	// Provider that always fails — exempt routes should never call it
	provider := &failProvider{}
	middleware := Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(inner)

	exempt := []string{
		"/-/health",
		"/-/ready",
		"/-/metrics/status",
		"/api",
		"/api/context.jsonld",
		"/api/scalar.js",
		"/assets/index.js",
		"/auth/callback",
	}
	for _, path := range exempt {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("%s: status=%d, want 200", path, w.Code)
		}
	}
}

func TestMiddleware_AuthError_Returns401(t *testing.T) {
	provider := &failProvider{}
	middleware := Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := middleware(inner)
	req := httptest.NewRequest("GET", "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("status=%d, want 401", w.Code)
	}
}

func TestMiddleware_ProviderHandlesResponse(t *testing.T) {
	provider := &redirectProvider{}
	middleware := Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler should not be called")
	})

	handler := middleware(inner)
	req := httptest.NewRequest("GET", "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 302 {
		t.Errorf("status=%d, want 302", w.Code)
	}
}

// --- test helpers ---

type failProvider struct{}

func (p *failProvider) Authenticate(_ http.ResponseWriter, _ *http.Request) (*Identity, error) {
	return nil, fmt.Errorf("auth failed")
}
func (p *failProvider) RegisterRoutes(_ *http.ServeMux) {}

type redirectProvider struct{}

func (p *redirectProvider) Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error) {
	http.Redirect(w, r, "/auth/login", http.StatusFound)
	return nil, nil
}
func (p *redirectProvider) RegisterRoutes(_ *http.ServeMux) {}
```

Note: add `"fmt"` to the imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestMiddleware -v`
Expected: FAIL — `Middleware` not defined

**Step 3: Write minimal implementation**

```go
// internal/auth/middleware.go
package auth

import (
	"log/slog"
	"net/http"
	"strings"
)

// Middleware returns an HTTP middleware that authenticates requests using the
// given provider. Exempt routes are passed through without authentication.
func Middleware(provider Provider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			id, err := provider.Authenticate(w, r)
			if err != nil {
				slog.Debug("authentication failed", "error", err, "path", r.URL.Path)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Provider handled the response (e.g., redirect)
			if id == nil {
				return
			}

			ctx := ContextWithIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isExempt(path string) bool {
	switch {
	case strings.HasPrefix(path, "/-/"):
		return true
	case path == "/api" || strings.HasPrefix(path, "/api/"):
		return true
	case strings.HasPrefix(path, "/assets/"):
		return true
	case strings.HasPrefix(path, "/auth/"):
		return true
	default:
		return false
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestMiddleware -v`
Expected: PASS

**Step 5: Run all auth tests**

Run: `go test ./internal/auth/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/auth/middleware.go internal/auth/middleware_test.go
git commit -m "feat(auth): add auth middleware with route exemptions"
```

---

### Task 4: Auth config parsing

**Files:**
- Modify: `internal/config/config.go:11-42`
- Create: `internal/config/auth.go`
- Create: `internal/config/auth_test.go`

**Step 1: Write the failing test**

```go
// internal/config/auth_test.go
package config

import (
	"testing"
)

func TestAuthConfig_Defaults(t *testing.T) {
	cfg, err := LoadAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mode != "none" {
		t.Errorf("mode=%q, want %q", cfg.Mode, "none")
	}
}

func TestAuthConfig_OIDC_RequiresFields(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	_, err := LoadAuth()
	if err == nil {
		t.Fatal("expected error for OIDC without required fields")
	}
}

func TestAuthConfig_OIDC_Valid(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://issuer.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "my-client")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "my-secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "https://cetacean.example.com/auth/callback")

	cfg, err := LoadAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://issuer.example.com" {
		t.Errorf("issuer=%q", cfg.OIDC.Issuer)
	}
}

func TestAuthConfig_Headers_RequiresSubject(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	_, err := LoadAuth()
	if err == nil {
		t.Fatal("expected error for headers without subject header")
	}
}

func TestAuthConfig_Headers_SecretRequiresBoth(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER", "X-Secret")
	// missing SECRET_VALUE
	_, err := LoadAuth()
	if err == nil {
		t.Fatal("expected error for secret header without secret value")
	}
}

func TestAuthConfig_Cert_RequiresCA(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "cert")
	_, err := LoadAuth()
	if err == nil {
		t.Fatal("expected error for cert without CA")
	}
}

func TestAuthConfig_Tailscale_Defaults(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "tailscale")
	cfg, err := LoadAuth()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tailscale.Mode != "local" {
		t.Errorf("tailscale mode=%q, want %q", cfg.Tailscale.Mode, "local")
	}
}

func TestAuthConfig_Tailscale_TsnetRequiresAuthkey(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "tailscale")
	t.Setenv("CETACEAN_AUTH_TAILSCALE_MODE", "tsnet")
	_, err := LoadAuth()
	if err == nil {
		t.Fatal("expected error for tsnet without authkey")
	}
}

func TestAuthConfig_InvalidMode(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "kerberos")
	_, err := LoadAuth()
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestAuthConfig -v`
Expected: FAIL — `LoadAuth` not defined

**Step 3: Write minimal implementation**

```go
// internal/config/auth.go
package config

import (
	"fmt"
	"os"
	"strings"
)

type AuthConfig struct {
	Mode      string          // "none", "oidc", "tailscale", "cert", "headers"
	OIDC      OIDCConfig
	Tailscale TailscaleConfig
	Cert      CertConfig
	Headers   HeadersConfig
}

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type TailscaleConfig struct {
	Mode     string // "local" or "tsnet"
	AuthKey  string
	Hostname string
	StateDir string
}

type CertConfig struct {
	CA string // path to CA bundle
}

type HeadersConfig struct {
	Subject      string // required header name for subject
	Name         string // optional header name for display name
	Email        string // optional header name for email
	Groups       string // optional header name for groups (comma-separated)
	SecretHeader string // optional: header name containing proxy secret
	SecretValue  string // required if SecretHeader is set
}

func LoadAuth() (*AuthConfig, error) {
	cfg := &AuthConfig{
		Mode: envOr("CETACEAN_AUTH_MODE", "none"),
	}

	switch cfg.Mode {
	case "none":
		// no additional config
	case "oidc":
		cfg.OIDC = OIDCConfig{
			Issuer:       os.Getenv("CETACEAN_AUTH_OIDC_ISSUER"),
			ClientID:     os.Getenv("CETACEAN_AUTH_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("CETACEAN_AUTH_OIDC_REDIRECT_URL"),
			Scopes:       parseScopes(envOr("CETACEAN_AUTH_OIDC_SCOPES", "openid,profile,email")),
		}
		if cfg.OIDC.Issuer == "" || cfg.OIDC.ClientID == "" || cfg.OIDC.ClientSecret == "" || cfg.OIDC.RedirectURL == "" {
			return nil, fmt.Errorf("OIDC mode requires CETACEAN_AUTH_OIDC_ISSUER, CLIENT_ID, CLIENT_SECRET, and REDIRECT_URL")
		}
	case "tailscale":
		cfg.Tailscale = TailscaleConfig{
			Mode:     envOr("CETACEAN_AUTH_TAILSCALE_MODE", "local"),
			AuthKey:  os.Getenv("CETACEAN_AUTH_TAILSCALE_AUTHKEY"),
			Hostname: envOr("CETACEAN_AUTH_TAILSCALE_HOSTNAME", "cetacean"),
			StateDir: os.Getenv("CETACEAN_AUTH_TAILSCALE_STATE_DIR"),
		}
		if cfg.Tailscale.Mode != "local" && cfg.Tailscale.Mode != "tsnet" {
			return nil, fmt.Errorf("CETACEAN_AUTH_TAILSCALE_MODE must be \"local\" or \"tsnet\"")
		}
		if cfg.Tailscale.Mode == "tsnet" && cfg.Tailscale.AuthKey == "" {
			return nil, fmt.Errorf("tsnet mode requires CETACEAN_AUTH_TAILSCALE_AUTHKEY")
		}
	case "cert":
		cfg.Cert = CertConfig{
			CA: os.Getenv("CETACEAN_AUTH_CERT_CA"),
		}
		if cfg.Cert.CA == "" {
			return nil, fmt.Errorf("cert mode requires CETACEAN_AUTH_CERT_CA")
		}
	case "headers":
		cfg.Headers = HeadersConfig{
			Subject:      os.Getenv("CETACEAN_AUTH_HEADERS_SUBJECT"),
			Name:         os.Getenv("CETACEAN_AUTH_HEADERS_NAME"),
			Email:        os.Getenv("CETACEAN_AUTH_HEADERS_EMAIL"),
			Groups:       os.Getenv("CETACEAN_AUTH_HEADERS_GROUPS"),
			SecretHeader: os.Getenv("CETACEAN_AUTH_HEADERS_SECRET_HEADER"),
			SecretValue:  os.Getenv("CETACEAN_AUTH_HEADERS_SECRET_VALUE"),
		}
		if cfg.Headers.Subject == "" {
			return nil, fmt.Errorf("headers mode requires CETACEAN_AUTH_HEADERS_SUBJECT")
		}
		if cfg.Headers.SecretHeader != "" && cfg.Headers.SecretValue == "" {
			return nil, fmt.Errorf("CETACEAN_AUTH_HEADERS_SECRET_VALUE required when SECRET_HEADER is set")
		}
	default:
		return nil, fmt.Errorf("unknown auth mode %q (valid: none, oidc, tailscale, cert, headers)", cfg.Mode)
	}

	return cfg, nil
}

func parseScopes(s string) []string {
	var scopes []string
	for _, scope := range strings.Split(s, ",") {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestAuthConfig -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/config/auth.go internal/config/auth_test.go
git commit -m "feat(auth): add auth config parsing with per-mode validation"
```

---

### Task 5: TLS config and server startup changes

**Files:**
- Modify: `internal/config/config.go:11-21` — add TLS fields
- Modify: `main.go:112-138` — conditional TLS + cert mode client auth
- Create: `internal/config/tls_test.go`

**Step 1: Write the failing test**

```go
// internal/config/tls_test.go
package config

import "testing"

func TestTLSConfig_Empty(t *testing.T) {
	cfg := LoadTLS()
	if cfg.Enabled() {
		t.Error("TLS should not be enabled without cert/key")
	}
}

func TestTLSConfig_RequiresBoth(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "./server.pem")
	// missing key
	_, err := ValidateTLS(LoadTLS())
	if err == nil {
		t.Fatal("expected error when cert is set without key")
	}
}

func TestTLSConfig_Valid(t *testing.T) {
	t.Setenv("CETACEAN_TLS_CERT", "./server.pem")
	t.Setenv("CETACEAN_TLS_KEY", "./server-key.pem")
	cfg := LoadTLS()
	if !cfg.Enabled() {
		t.Error("TLS should be enabled")
	}
	if _, err := ValidateTLS(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestTLS -v`
Expected: FAIL

**Step 3: Implement TLS config**

Add to `internal/config/config.go` — new struct fields and a TLS sub-config:

```go
type TLSConfig struct {
	Cert string // CETACEAN_TLS_CERT
	Key  string // CETACEAN_TLS_KEY
}

func (t TLSConfig) Enabled() bool {
	return t.Cert != "" || t.Key != ""
}

func LoadTLS() TLSConfig {
	return TLSConfig{
		Cert: os.Getenv("CETACEAN_TLS_CERT"),
		Key:  os.Getenv("CETACEAN_TLS_KEY"),
	}
}

func ValidateTLS(cfg TLSConfig) (TLSConfig, error) {
	if (cfg.Cert == "") != (cfg.Key == "") {
		return cfg, fmt.Errorf("CETACEAN_TLS_CERT and CETACEAN_TLS_KEY must both be set or both unset")
	}
	return cfg, nil
}
```

Add `TLS TLSConfig` to the `Config` struct and wire `LoadTLS()` + `ValidateTLS()` into `Load()`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: All PASS

**Step 5: Update main.go server startup**

Modify `main.go:114-138` to conditionally use `ListenAndServeTLS`:

```go
// After building cfg and authCfg...
if cfg.TLS.Enabled() {
	slog.Info("TLS enabled", "cert", cfg.TLS.Cert, "key", cfg.TLS.Key)
	if err := server.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
} else {
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
```

**Step 6: Run full test suite**

Run: `go test ./...`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/config/config.go internal/config/tls_test.go main.go
git commit -m "feat: add TLS termination support via CETACEAN_TLS_CERT/KEY"
```

---

### Task 6: Wire auth middleware into router and main.go

**Files:**
- Modify: `internal/api/router.go:8,93-100` — accept Provider, insert middleware, call RegisterRoutes
- Modify: `main.go:32-138` — load auth config, build provider, pass to router

**Step 1: Write the failing test**

Add to `internal/auth/middleware_test.go`:

```go
func TestMiddleware_IntegrationWithRouter(t *testing.T) {
	// Verify the middleware signature is compatible with the existing
	// func(http.Handler) http.Handler middleware chain pattern
	provider := &NoneProvider{}
	mw := Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := IdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)
	req := httptest.NewRequest("GET", "/nodes", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status=%d, want 200", w.Code)
	}
}
```

**Step 2: Modify `NewRouter` signature**

Change `internal/api/router.go:8`:

```go
func NewRouter(h *Handlers, b *Broadcaster, promProxy http.Handler, spa http.Handler, openapiSpec []byte, scalarJS []byte, enablePprof bool, authMiddleware func(http.Handler) http.Handler) http.Handler {
```

Insert the auth middleware into the chain at `router.go:93-100`:

```go
var handler http.Handler = mux
handler = requestLogger(handler)
handler = discoveryLinks(handler)
handler = negotiate(handler)
handler = authMiddleware(handler)    // ← new
handler = securityHeaders(handler)
handler = recovery(handler)
handler = requestID(handler)
return handler
```

**Step 3: Update main.go to build provider and pass middleware**

In `main.go`, after config loading, add:

```go
authCfg, err := config.LoadAuth()
if err != nil {
	fmt.Fprintf(os.Stderr, "auth configuration error: %v\n", err)
	os.Exit(1)
}

// Build auth provider
var authProvider auth.Provider
switch authCfg.Mode {
case "none":
	authProvider = &auth.NoneProvider{}
// other modes will be added in subsequent tasks
default:
	fmt.Fprintf(os.Stderr, "auth mode %q not yet implemented\n", authCfg.Mode)
	os.Exit(1)
}

authMW := auth.Middleware(authProvider)
```

Pass `authMW` to `NewRouter`:

```go
router := api.NewRouter(handlers, broadcaster, promProxy, spa, openapiSpec, scalarJS, cfg.Pprof, authMW)
```

Also call `authProvider.RegisterRoutes(...)` — this requires the mux to be accessible. Simplest: let `NewRouter` call `provider.RegisterRoutes` internally, or pass the provider. Since we already pass the middleware function, pass the provider instead and let `NewRouter` build the middleware and call RegisterRoutes:

Actually, simpler: change `NewRouter` to accept `auth.Provider` directly:

```go
func NewRouter(h *Handlers, b *Broadcaster, promProxy http.Handler, spa http.Handler, openapiSpec []byte, scalarJS []byte, enablePprof bool, authProvider auth.Provider) http.Handler {
    mux := http.NewServeMux()

    // Register auth routes before other routes
    authProvider.RegisterRoutes(mux)

    // ... existing route registrations ...

    authMW := auth.Middleware(authProvider)
    // ... middleware chain with authMW ...
}
```

**Step 4: Run full test suite**

Run: `go test ./...`
Expected: All PASS (update any test that calls `NewRouter` to pass a `&auth.NoneProvider{}`)

**Step 5: Commit**

```bash
git add internal/api/router.go main.go
git commit -m "feat(auth): wire auth middleware into router and main.go"
```

---

### Task 7: Signed cookie session (for OIDC)

**Files:**
- Create: `internal/auth/session.go`
- Create: `internal/auth/session_test.go`

**Step 1: Write the failing tests**

```go
// internal/auth/session_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSession_RoundTrip(t *testing.T) {
	s := NewSessionCodec()
	id := &Identity{
		Subject:     "user-123",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Groups:      []string{"admin"},
		Provider:    "oidc",
	}

	w := httptest.NewRecorder()
	s.Set(w, id, time.Hour)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])
	got, err := s.Get(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Subject != "user-123" {
		t.Errorf("subject=%q, want %q", got.Subject, "user-123")
	}
	if got.Email != "alice@example.com" {
		t.Errorf("email=%q", got.Email)
	}
}

func TestSession_TamperedCookie(t *testing.T) {
	s := NewSessionCodec()
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "cetacean_session", Value: "tampered-value"})
	_, err := s.Get(req)
	if err == nil {
		t.Fatal("expected error for tampered cookie")
	}
}

func TestSession_DifferentKeys(t *testing.T) {
	s1 := NewSessionCodec()
	s2 := NewSessionCodec() // different ephemeral key

	id := &Identity{Subject: "user-123", Provider: "oidc"}
	w := httptest.NewRecorder()
	s1.Set(w, id, time.Hour)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(w.Result().Cookies()[0])
	_, err := s2.Get(req)
	if err == nil {
		t.Fatal("expected error: different signing keys")
	}
}

func TestSession_Clear(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewSessionCodec()
	s.Clear(w)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie")
	}
	if cookies[0].MaxAge >= 0 {
		t.Errorf("expected negative MaxAge, got %d", cookies[0].MaxAge)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestSession -v`
Expected: FAIL

**Step 3: Implement session codec**

```go
// internal/auth/session.go
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	json "github.com/goccy/go-json"
)

const cookieName = "cetacean_session"

type SessionCodec struct {
	key []byte
}

func NewSessionCodec() *SessionCodec {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("failed to generate session key: " + err.Error())
	}
	return &SessionCodec{key: key}
}

func (s *SessionCodec) Set(w http.ResponseWriter, id *Identity, ttl time.Duration) {
	payload, _ := json.Marshal(id)
	sig := s.sign(payload)
	value := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig)

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *SessionCodec) Get(r *http.Request) (*Identity, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil, err
	}

	parts := splitOnce(cookie.Value, '.')
	if len(parts) != 2 {
		return nil, errors.New("malformed session cookie")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("malformed session payload")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed session signature")
	}

	if !hmac.Equal(s.sign(payload), sig) {
		return nil, errors.New("invalid session signature")
	}

	var id Identity
	if err := json.Unmarshal(payload, &id); err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *SessionCodec) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

func (s *SessionCodec) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write(data)
	return mac.Sum(nil)
}

func splitOnce(s string, sep byte) []string {
	for i := range len(s) {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestSession -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/auth/session.go internal/auth/session_test.go
git commit -m "feat(auth): add HMAC-signed session cookie codec"
```

---

### Task 8: Headers provider

**Files:**
- Create: `internal/auth/headers.go`
- Create: `internal/auth/headers_test.go`

**Step 1: Write the failing tests**

```go
// internal/auth/headers_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

func TestHeadersProvider_BasicAuth(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject: "X-User",
		Name:    "X-Name",
		Email:   "X-Email",
		Groups:  "X-Groups",
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User", "alice")
	req.Header.Set("X-Name", "Alice Smith")
	req.Header.Set("X-Email", "alice@example.com")
	req.Header.Set("X-Groups", "admin,users")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "alice" {
		t.Errorf("subject=%q", id.Subject)
	}
	if id.DisplayName != "Alice Smith" {
		t.Errorf("name=%q", id.DisplayName)
	}
	if len(id.Groups) != 2 || id.Groups[0] != "admin" {
		t.Errorf("groups=%v", id.Groups)
	}
}

func TestHeadersProvider_MissingSubject(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{Subject: "X-User"})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	_, err := p.Authenticate(w, req)
	if err == nil {
		t.Fatal("expected error for missing subject header")
	}
}

func TestHeadersProvider_SecretValid(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject:      "X-User",
		SecretHeader: "X-Proxy-Secret",
		SecretValue:  "s3cret",
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User", "alice")
	req.Header.Set("X-Proxy-Secret", "s3cret")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "alice" {
		t.Errorf("subject=%q", id.Subject)
	}
}

func TestHeadersProvider_SecretInvalid(t *testing.T) {
	p := NewHeadersProvider(config.HeadersConfig{
		Subject:      "X-User",
		SecretHeader: "X-Proxy-Secret",
		SecretValue:  "s3cret",
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User", "alice")
	req.Header.Set("X-Proxy-Secret", "wrong")
	w := httptest.NewRecorder()

	_, err := p.Authenticate(w, req)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestHeadersProvider -v`
Expected: FAIL

**Step 3: Implement**

```go
// internal/auth/headers.go
package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/radiergummi/cetacean/internal/config"
)

type HeadersProvider struct {
	cfg config.HeadersConfig
}

func NewHeadersProvider(cfg config.HeadersConfig) *HeadersProvider {
	return &HeadersProvider{cfg: cfg}
}

func (p *HeadersProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	if p.cfg.SecretHeader != "" {
		got := r.Header.Get(p.cfg.SecretHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(p.cfg.SecretValue)) != 1 {
			return nil, errors.New("invalid proxy secret")
		}
	}

	subject := r.Header.Get(p.cfg.Subject)
	if subject == "" {
		return nil, errors.New("missing subject header")
	}

	id := &Identity{
		Subject:  subject,
		Provider: "headers",
		Raw:      make(map[string]any),
	}

	if p.cfg.Name != "" {
		id.DisplayName = r.Header.Get(p.cfg.Name)
	}
	if id.DisplayName == "" {
		id.DisplayName = subject
	}

	if p.cfg.Email != "" {
		id.Email = r.Header.Get(p.cfg.Email)
	}

	if p.cfg.Groups != "" {
		if g := r.Header.Get(p.cfg.Groups); g != "" {
			for _, group := range strings.Split(g, ",") {
				group = strings.TrimSpace(group)
				if group != "" {
					id.Groups = append(id.Groups, group)
				}
			}
		}
	}

	// Populate Raw with all matched headers
	id.Raw["subject_header"] = p.cfg.Subject
	id.Raw["subject"] = subject

	return id, nil
}

func (p *HeadersProvider) RegisterRoutes(_ *http.ServeMux) {}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestHeadersProvider -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/auth/headers.go internal/auth/headers_test.go
git commit -m "feat(auth): add headers provider for trusted proxy auth"
```

---

### Task 9: Cert provider

**Files:**
- Create: `internal/auth/cert.go`
- Create: `internal/auth/cert_test.go`

**Step 1: Write the failing tests**

```go
// internal/auth/cert_test.go
package auth

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCertProvider_CN(t *testing.T) {
	p := &CertProvider{}
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{
			Subject: pkix.Name{
				CommonName:         "alice",
				OrganizationalUnit: []string{"engineering", "platform"},
			},
			EmailAddresses: []string{"alice@example.com"},
		}},
	}
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "alice" {
		t.Errorf("subject=%q", id.Subject)
	}
	if id.Email != "alice@example.com" {
		t.Errorf("email=%q", id.Email)
	}
	if len(id.Groups) != 2 || id.Groups[0] != "engineering" {
		t.Errorf("groups=%v", id.Groups)
	}
}

func TestCertProvider_SPIFFE(t *testing.T) {
	p := &CertProvider{}
	spiffeURI, _ := url.Parse("spiffe://cluster.local/ns/default/sa/worker")
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{
			URIs:    []*url.URL{spiffeURI},
			Subject: pkix.Name{CommonName: "worker"},
		}},
	}
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "spiffe://cluster.local/ns/default/sa/worker" {
		t.Errorf("subject=%q", id.Subject)
	}
}

func TestCertProvider_NoCert(t *testing.T) {
	p := &CertProvider{}
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	_, err := p.Authenticate(w, req)
	if err == nil {
		t.Fatal("expected error for missing client cert")
	}
}

func TestCertProvider_NoTLS(t *testing.T) {
	p := &CertProvider{}
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = nil
	w := httptest.NewRecorder()

	_, err := p.Authenticate(w, req)
	if err == nil {
		t.Fatal("expected error for non-TLS connection")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestCertProvider -v`
Expected: FAIL

**Step 3: Implement**

```go
// internal/auth/cert.go
package auth

import (
	"errors"
	"net/http"
)

type CertProvider struct{}

func (p *CertProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil, errors.New("client certificate required")
	}

	cert := r.TLS.PeerCertificates[0]
	id := &Identity{
		Provider: "cert",
		Raw:      make(map[string]any),
	}

	// Check for SPIFFE URI SAN first
	for _, uri := range cert.URIs {
		if uri.Scheme == "spiffe" {
			id.Subject = uri.String()
			id.DisplayName = cert.Subject.CommonName
			id.Raw["spiffe_id"] = uri.String()
			break
		}
	}

	// Fall back to CN
	if id.Subject == "" {
		id.Subject = cert.Subject.CommonName
		id.DisplayName = cert.Subject.CommonName
	}

	// Email from SAN
	if len(cert.EmailAddresses) > 0 {
		id.Email = cert.EmailAddresses[0]
	}

	// Groups from OU
	id.Groups = cert.Subject.OrganizationalUnit

	id.Raw["serial"] = cert.SerialNumber.String()
	id.Raw["issuer"] = cert.Issuer.CommonName
	id.Raw["not_after"] = cert.NotAfter.String()

	return id, nil
}

func (p *CertProvider) RegisterRoutes(_ *http.ServeMux) {}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestCertProvider -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/auth/cert.go internal/auth/cert_test.go
git commit -m "feat(auth): add cert provider with SPIFFE support"
```

---

### Task 10: OIDC provider

**Files:**
- Create: `internal/auth/oidc.go`
- Create: `internal/auth/oidc_test.go`

This is the most complex provider. It has two code paths (browser + bearer) and needs external deps.

**Step 1: Add dependencies**

Run:
```bash
go get github.com/coreos/go-oidc/v3/oidc
go get golang.org/x/oauth2
```

**Step 2: Write the failing tests**

```go
// internal/auth/oidc_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOIDCProvider_RedirectsUnauthenticatedBrowser(t *testing.T) {
	// Use a mock OIDC provider (httptest server serving .well-known/openid-configuration)
	oidcServer := newMockOIDCServer(t)
	defer oidcServer.Close()

	p, err := NewOIDCProvider(OIDCProviderConfig{
		Issuer:       oidcServer.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:9000/auth/callback",
		Scopes:       []string{"openid", "profile", "email"},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	req := httptest.NewRequest("GET", "/nodes", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != nil {
		t.Fatal("expected nil identity for redirect")
	}
	if w.Code != 302 {
		t.Errorf("status=%d, want 302", w.Code)
	}
}

func TestOIDCProvider_Returns401ForAPIRequest(t *testing.T) {
	oidcServer := newMockOIDCServer(t)
	defer oidcServer.Close()

	p, err := NewOIDCProvider(OIDCProviderConfig{
		Issuer:       oidcServer.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:9000/auth/callback",
		Scopes:       []string{"openid"},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	req := httptest.NewRequest("GET", "/nodes", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	_, err = p.Authenticate(w, req)
	if err == nil {
		t.Fatal("expected error for unauthenticated API request")
	}
}

func TestOIDCProvider_ValidSessionCookie(t *testing.T) {
	oidcServer := newMockOIDCServer(t)
	defer oidcServer.Close()

	p, err := NewOIDCProvider(OIDCProviderConfig{
		Issuer:       oidcServer.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:9000/auth/callback",
		Scopes:       []string{"openid"},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	// Manually set a valid session cookie
	id := &Identity{Subject: "user-123", Provider: "oidc", DisplayName: "Alice"}
	w := httptest.NewRecorder()
	p.session.Set(w, id, 3600)
	cookies := w.Result().Cookies()

	req := httptest.NewRequest("GET", "/nodes", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w2 := httptest.NewRecorder()

	got, err := p.Authenticate(w2, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Subject != "user-123" {
		t.Errorf("subject=%q", got.Subject)
	}
}

func TestOIDCProvider_RegisterRoutes(t *testing.T) {
	oidcServer := newMockOIDCServer(t)
	defer oidcServer.Close()

	p, err := NewOIDCProvider(OIDCProviderConfig{
		Issuer:       oidcServer.URL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:9000/auth/callback",
		Scopes:       []string{"openid"},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	mux := http.NewServeMux()
	p.RegisterRoutes(mux)

	// Verify /auth/login exists
	req := httptest.NewRequest("GET", "/auth/login", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 302 {
		t.Errorf("/auth/login status=%d, want 302", w.Code)
	}
}
```

The test helper `newMockOIDCServer` should serve a minimal `.well-known/openid-configuration` and JWKS endpoint:

```go
func newMockOIDCServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var serverURL string

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q,
			"id_token_signing_alg_values_supported": ["RS256"]
		}`, serverURL, serverURL+"/authorize", serverURL+"/token", serverURL+"/jwks")
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	})

	server := httptest.NewServer(mux)
	serverURL = server.URL
	return server
}
```

Note: add `"fmt"` to imports.

**Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestOIDCProvider -v`
Expected: FAIL — `NewOIDCProvider` not defined

**Step 4: Implement OIDC provider**

```go
// internal/auth/oidc.go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const maxSessionTTL = 8 * time.Hour

type OIDCProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type OIDCProvider struct {
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	session      *SessionCodec
}

func NewOIDCProvider(cfg OIDCProviderConfig) (*OIDCProvider, error) {
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}

	oauth2Config := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.Scopes,
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})

	return &OIDCProvider{
		oauth2Config: oauth2Config,
		verifier:     verifier,
		session:      NewSessionCodec(),
	}, nil
}

func (p *OIDCProvider) Authenticate(w http.ResponseWriter, r *http.Request) (*Identity, error) {
	// Check for existing session cookie first
	if id, err := p.session.Get(r); err == nil {
		return id, nil
	}

	// Check for Bearer token
	if token := extractBearerToken(r); token != "" {
		return p.validateBearerToken(r.Context(), token)
	}

	// No credentials — redirect browsers, reject API clients
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		state := generateState()
		http.SetCookie(w, &http.Cookie{
			Name:     "cetacean_auth_state",
			Value:    state,
			Path:     "/auth",
			MaxAge:   300,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     "cetacean_auth_redirect",
			Value:    r.URL.RequestURI(),
			Path:     "/auth",
			MaxAge:   300,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, p.oauth2Config.AuthCodeURL(state), http.StatusFound)
		return nil, nil
	}

	return nil, errors.New("authentication required")
}

func (p *OIDCProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", p.handleLogin)
	mux.HandleFunc("GET /auth/callback", p.handleCallback)
	mux.HandleFunc("GET /auth/logout", p.handleLogout)
	mux.HandleFunc("GET /auth/whoami", p.handleWhoami)
}

func (p *OIDCProvider) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_state",
		Value:    state,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "cetacean_auth_redirect",
		Value:    redirect,
		Path:     "/auth",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, p.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

func (p *OIDCProvider) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state
	stateCookie, err := r.Cookie("cetacean_auth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens
	token, err := p.oauth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	// Extract ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusInternalServerError)
		return
	}

	idToken, err := p.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "invalid id_token", http.StatusInternalServerError)
		return
	}

	// Extract claims
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}

	id := claimsToIdentity(claims)

	// Set session cookie
	ttl := time.Until(token.Expiry)
	if ttl > maxSessionTTL {
		ttl = maxSessionTTL
	}
	if ttl <= 0 {
		ttl = maxSessionTTL
	}
	p.session.Set(w, id, ttl)

	// Clear state cookies
	http.SetCookie(w, &http.Cookie{Name: "cetacean_auth_state", Path: "/auth", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "cetacean_auth_redirect", Path: "/auth", MaxAge: -1})

	// Redirect to original URL
	redirect := "/"
	if c, err := r.Cookie("cetacean_auth_redirect"); err == nil && c.Value != "" {
		redirect = c.Value
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (p *OIDCProvider) handleLogout(w http.ResponseWriter, r *http.Request) {
	p.session.Clear(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (p *OIDCProvider) handleWhoami(w http.ResponseWriter, r *http.Request) {
	id, err := p.session.Get(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(id)
}

func (p *OIDCProvider) validateBearerToken(ctx context.Context, token string) (*Identity, error) {
	idToken, err := p.verifier.Verify(ctx, token)
	if err != nil {
		return nil, err
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}

	return claimsToIdentity(claims), nil
}

func claimsToIdentity(claims map[string]any) *Identity {
	id := &Identity{
		Provider: "oidc",
		Raw:      claims,
	}

	if sub, ok := claims["sub"].(string); ok {
		id.Subject = sub
	}
	if name, ok := claims["name"].(string); ok {
		id.DisplayName = name
	}
	if email, ok := claims["email"].(string); ok {
		id.Email = email
	}
	if groups, ok := claims["groups"].([]any); ok {
		for _, g := range groups {
			if s, ok := g.(string); ok {
				id.Groups = append(id.Groups, s)
			}
		}
	}

	return id
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

Note: add `json "github.com/goccy/go-json"` to imports for `handleWhoami`.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestOIDCProvider -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/auth/oidc.go internal/auth/oidc_test.go
git commit -m "feat(auth): add OIDC provider with auth code flow and bearer token support"
```

---

### Task 11: Tailscale provider (local mode)

**Files:**
- Create: `internal/auth/tailscale.go`
- Create: `internal/auth/tailscale_test.go`

**Step 1: Add dependency**

Run:
```bash
go get tailscale.com/client/tailscale
go get tailscale.com/tsnet
```

**Step 2: Write the failing tests**

The Tailscale local API requires a running daemon, so tests use an interface for the `WhoIs` call:

```go
// internal/auth/tailscale_test.go
package auth

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"tailscale.com/apitype"
	"tailscale.com/tailcfg"
)

type mockWhoIsClient struct {
	result *apitype.WhoIsResponse
	err    error
}

func (m *mockWhoIsClient) WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	return m.result, m.err
}

func TestTailscaleProvider_LocalMode(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			result: &apitype.WhoIsResponse{
				UserProfile: &tailcfg.UserProfile{
					ID:          12345,
					LoginName:   "alice@example.com",
					DisplayName: "Alice Smith",
				},
				Node: &tailcfg.Node{
					Name: "alice-laptop.tail12345.ts.net.",
				},
			},
		},
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "100.64.0.1:12345"
	w := httptest.NewRecorder()

	id, err := p.Authenticate(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Subject != "12345" {
		t.Errorf("subject=%q", id.Subject)
	}
	if id.DisplayName != "Alice Smith" {
		t.Errorf("name=%q", id.DisplayName)
	}
	if id.Email != "alice@example.com" {
		t.Errorf("email=%q", id.Email)
	}
	if id.Provider != "tailscale" {
		t.Errorf("provider=%q", id.Provider)
	}
}

func TestTailscaleProvider_NonTailscaleIP(t *testing.T) {
	p := &TailscaleProvider{
		client: &mockWhoIsClient{
			err: fmt.Errorf("not a tailscale IP"),
		},
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	_, err := p.Authenticate(w, req)
	if err == nil {
		t.Fatal("expected error for non-tailscale IP")
	}
}
```

Note: add `"fmt"` to imports.

**Step 3: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestTailscaleProvider -v`
Expected: FAIL

**Step 4: Implement**

```go
// internal/auth/tailscale.go
package auth

import (
	"context"
	"fmt"
	"net/http"

	"tailscale.com/apitype"
	"tailscale.com/client/tailscale"
)

// WhoIsClient abstracts the Tailscale WhoIs API for testing.
type WhoIsClient interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

type TailscaleProvider struct {
	client WhoIsClient
}

// NewTailscaleLocalProvider creates a provider using the local Tailscale daemon.
func NewTailscaleLocalProvider() *TailscaleProvider {
	return &TailscaleProvider{
		client: &tailscale.LocalClient{},
	}
}

func (p *TailscaleProvider) Authenticate(_ http.ResponseWriter, r *http.Request) (*Identity, error) {
	who, err := p.client.WhoIs(r.Context(), r.RemoteAddr)
	if err != nil {
		return nil, fmt.Errorf("tailscale WhoIs: %w", err)
	}

	id := &Identity{
		Provider: "tailscale",
		Raw:      make(map[string]any),
	}

	if who.UserProfile != nil {
		id.Subject = fmt.Sprintf("%d", who.UserProfile.ID)
		id.DisplayName = who.UserProfile.DisplayName
		id.Email = who.UserProfile.LoginName
		id.Raw["user_id"] = who.UserProfile.ID
		id.Raw["login_name"] = who.UserProfile.LoginName
	}

	if who.Node != nil {
		id.Raw["node_name"] = who.Node.Name
	}

	return id, nil
}

func (p *TailscaleProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", func(w http.ResponseWriter, r *http.Request) {
		id, err := p.Authenticate(w, r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(id)
	})
}
```

Note: add `json "github.com/goccy/go-json"` to imports.

**Step 5: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestTailscaleProvider -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/auth/tailscale.go internal/auth/tailscale_test.go
git commit -m "feat(auth): add Tailscale provider with local daemon support"
```

---

### Task 12: Tailscale tsnet mode

**Files:**
- Modify: `internal/auth/tailscale.go` — add tsnet server creation
- Modify: `main.go` — dual listener for tsnet mode

**Step 1: Add tsnet listener creation**

```go
// Add to internal/auth/tailscale.go

import "tailscale.com/tsnet"

type TsnetServer struct {
	*tsnet.Server
}

// NewTailscaleTsnetProvider creates a provider using an embedded tsnet node.
// The caller must use the returned listener for the main server and also
// serve meta endpoints on the regular listener.
func NewTailscaleTsnetProvider(hostname, authKey, stateDir string) (*TailscaleProvider, *TsnetServer, error) {
	srv := &tsnet.Server{
		Hostname: hostname,
		AuthKey:  authKey,
		Dir:      stateDir,
	}

	lc, err := srv.LocalClient()
	if err != nil {
		return nil, nil, fmt.Errorf("tsnet local client: %w", err)
	}

	provider := &TailscaleProvider{client: lc}
	return provider, &TsnetServer{srv}, nil
}
```

**Step 2: Update main.go for dual listener**

In main.go, when tsnet mode is configured:

```go
// Pseudocode for the tsnet listener wiring in main.go:
// 1. Create tsnet provider + server
// 2. Get tsnet listener via server.Listen("tcp", ":443") or ":80"
// 3. Create a meta-only mux for the regular listener (just /-/ routes)
// 4. Serve meta mux on regular listener, full router on tsnet listener
```

This is wiring-level code that depends on how the rest of the main.go evolves. The implementation should:
- Start the tsnet server
- Get a `net.Listener` from it
- Serve the full router on that listener
- Serve a minimal `/-/` only handler on the regular `cfg.ListenAddr`

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/auth/tailscale.go main.go
git commit -m "feat(auth): add tsnet mode with dual listener support"
```

---

### Task 13: Wire all providers in main.go

**Files:**
- Modify: `main.go` — complete the provider switch

**Step 1: Complete the provider factory**

```go
// In main.go, replace the stub switch with:
var authProvider auth.Provider
switch authCfg.Mode {
case "none":
	authProvider = &auth.NoneProvider{}
case "oidc":
	authProvider, err = auth.NewOIDCProvider(auth.OIDCProviderConfig{
		Issuer:       authCfg.OIDC.Issuer,
		ClientID:     authCfg.OIDC.ClientID,
		ClientSecret: authCfg.OIDC.ClientSecret,
		RedirectURL:  authCfg.OIDC.RedirectURL,
		Scopes:       authCfg.OIDC.Scopes,
	})
	if err != nil {
		slog.Error("OIDC provider setup failed", "error", err)
		os.Exit(1)
	}
case "tailscale":
	if authCfg.Tailscale.Mode == "tsnet" {
		// tsnet mode — see Task 12 for dual listener wiring
		authProvider, tsnetServer, err = auth.NewTailscaleTsnetProvider(
			authCfg.Tailscale.Hostname,
			authCfg.Tailscale.AuthKey,
			authCfg.Tailscale.StateDir,
		)
		if err != nil {
			slog.Error("tsnet setup failed", "error", err)
			os.Exit(1)
		}
		defer tsnetServer.Close()
	} else {
		authProvider = auth.NewTailscaleLocalProvider()
	}
case "cert":
	authProvider = &auth.CertProvider{}
	// TLS client auth config is applied to server.TLSConfig
case "headers":
	authProvider = auth.NewHeadersProvider(authCfg.Headers)
}
```

For cert mode, also configure `server.TLSConfig`:

```go
if authCfg.Mode == "cert" {
	caCert, err := os.ReadFile(authCfg.Cert.CA)
	if err != nil {
		slog.Error("failed to read CA cert", "error", err)
		os.Exit(1)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		slog.Error("failed to parse CA cert")
		os.Exit(1)
	}
	server.TLSConfig = &tls.Config{
		ClientCAs:  caPool,
		ClientAuth: tls.RequireAndVerifyClientCert,
	}
}
```

**Step 2: Run full test suite**

Run: `go test ./...`
Expected: All PASS

**Step 3: Commit**

```bash
git add main.go
git commit -m "feat(auth): wire all auth providers in main.go"
```

---

### Task 14: Whoami endpoint for non-OIDC providers

The OIDC and Tailscale providers register their own `/auth/whoami` via `RegisterRoutes`. For other modes (none, headers, cert), the middleware already puts the identity in context, so we need a generic whoami handler.

**Files:**
- Create: `internal/auth/whoami.go`
- Create: `internal/auth/whoami_test.go`

**Step 1: Write the failing test**

```go
// internal/auth/whoami_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleWhoami_Authenticated(t *testing.T) {
	id := &Identity{Subject: "alice", Provider: "headers", DisplayName: "Alice"}
	req := httptest.NewRequest("GET", "/auth/whoami", nil)
	req = req.WithContext(ContextWithIdentity(req.Context(), id))
	w := httptest.NewRecorder()

	HandleWhoami(w, req)

	if w.Code != 200 {
		t.Errorf("status=%d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type=%q", ct)
	}
}

func TestHandleWhoami_Unauthenticated(t *testing.T) {
	req := httptest.NewRequest("GET", "/auth/whoami", nil)
	w := httptest.NewRecorder()

	HandleWhoami(w, req)

	if w.Code != 401 {
		t.Errorf("status=%d, want 401", w.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestHandleWhoami -v`
Expected: FAIL

**Step 3: Implement**

```go
// internal/auth/whoami.go
package auth

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// HandleWhoami returns the current identity from context as JSON.
// Used as a fallback for providers that don't register their own /auth/whoami.
func HandleWhoami(w http.ResponseWriter, r *http.Request) {
	id := IdentityFromContext(r.Context())
	if id == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(id)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -run TestHandleWhoami -v`
Expected: All PASS

**Step 5: Register in router**

In `internal/api/router.go`, after `authProvider.RegisterRoutes(mux)`, add a fallback whoami registration. The simplest approach: always register it on the mux; OIDC/Tailscale providers register theirs first (more specific), and Go's mux picks the first registered handler. Actually, `http.ServeMux` panics on duplicate patterns. So instead: register `/auth/whoami` in the router only if the provider doesn't claim it. Or simpler: have every provider's `RegisterRoutes` register `/auth/whoami` themselves. The NoneProvider, HeadersProvider, and CertProvider can all register the generic `HandleWhoami`.

Update `none.go`, `headers.go`, `cert.go` `RegisterRoutes`:

```go
func (p *NoneProvider) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/whoami", HandleWhoami)
}
```

(Same for HeadersProvider and CertProvider.)

**Step 6: Run full test suite**

Run: `go test ./...`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/auth/whoami.go internal/auth/whoami_test.go internal/auth/none.go internal/auth/headers.go internal/auth/cert.go
git commit -m "feat(auth): add /auth/whoami endpoint for all providers"
```

---

### Task 15: Frontend auth integration

**Files:**
- Modify: `frontend/src/api/client.ts` — add whoami call, 401 handling
- Modify: `frontend/src/api/types.ts` — add Identity type
- Create: `frontend/src/hooks/useAuth.ts` — auth context hook
- Modify: `frontend/src/App.tsx` (or equivalent root) — wrap with auth provider

**Step 1: Add Identity type**

```ts
// Add to frontend/src/api/types.ts
export interface Identity {
  subject: string;
  displayName: string;
  email?: string;
  groups?: string[];
  provider: string;
  raw?: Record<string, unknown>;
}
```

**Step 2: Add whoami to API client**

```ts
// Add to frontend/src/api/client.ts
whoami: () => fetchJSON<Identity>("/auth/whoami"),
```

**Step 3: Add 401 handling to fetchJSON**

```ts
// In fetchJSON, after the !res.ok check:
if (res.status === 401) {
  // Redirect to login for OIDC, show error for others
  window.location.href = "/auth/login?redirect=" + encodeURIComponent(window.location.pathname);
  throw new Error("Authentication required");
}
```

Note: This redirect-on-401 approach works for OIDC mode. For other modes, the redirect to `/auth/login` won't exist, so the SPA should check the whoami response first and adapt. A more nuanced approach: try `/auth/whoami` at startup. If it returns 401, check if `/auth/login` exists (or derive from the identity `provider` field) and redirect only for OIDC. For other modes, show an error banner.

**Step 4: Create useAuth hook**

```ts
// frontend/src/hooks/useAuth.ts
import { createContext, useContext, useEffect, useState } from "react";
import { api } from "@/api/client";
import type { Identity } from "@/api/types";

interface AuthState {
  identity: Identity | null;
  loading: boolean;
  error: string | null;
}

const AuthContext = createContext<AuthState>({
  identity: null,
  loading: true,
  error: null,
});

export function useAuth() {
  return useContext(AuthContext);
}

export { AuthContext };

export function useAuthLoader(): AuthState {
  const [state, setState] = useState<AuthState>({
    identity: null,
    loading: true,
    error: null,
  });

  useEffect(() => {
    api.whoami()
      .then((identity) => setState({ identity, loading: false, error: null }))
      .catch((err) => setState({ identity: null, loading: false, error: err.message }));
  }, []);

  return state;
}
```

**Step 5: Display identity in nav**

Update the nav bar component to show the current user's display name or email when authenticated. This is UI work that depends on the existing nav layout — adapt to the current component structure.

**Step 6: Run frontend checks**

Run:
```bash
cd frontend && npx tsc -b --noEmit && npm run lint
```
Expected: No errors

**Step 7: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/api/types.ts frontend/src/hooks/useAuth.ts
git commit -m "feat(auth): add frontend auth integration with whoami and useAuth hook"
```

---

### Task 16: Update OpenAPI spec

**Files:**
- Modify: `api/openapi.yaml` — add auth-related endpoints and security schemes

**Step 1: Add security schemes**

Add to the OpenAPI spec:
- `securitySchemes` section with `cookieAuth` and `bearerAuth`
- New endpoints: `/auth/login`, `/auth/callback`, `/auth/logout`, `/auth/whoami`
- Security requirement on all non-meta endpoints

**Step 2: Add auth endpoint schemas**

```yaml
/auth/whoami:
  get:
    summary: Get current identity
    operationId: whoami
    responses:
      '200':
        description: Current user identity
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Identity'
      '401':
        description: Not authenticated

components:
  schemas:
    Identity:
      type: object
      required: [subject, displayName, provider]
      properties:
        subject:
          type: string
        displayName:
          type: string
        email:
          type: string
        groups:
          type: array
          items:
            type: string
        provider:
          type: string
          enum: [none, oidc, tailscale, cert, headers]
        raw:
          type: object
          additionalProperties: true
```

**Step 3: Run OpenAPI validation**

Run: `go test ./internal/api/ -run TestOpenAPI -v`
Expected: PASS (if there's an OpenAPI validation test)

**Step 4: Commit**

```bash
git add api/openapi.yaml
git commit -m "docs(api): add authentication endpoints and security schemes to OpenAPI spec"
```

---

### Task 17: Update CLAUDE.md and environment variable docs

**Files:**
- Modify: `CLAUDE.md` — add auth section, update env var table, update middleware chain description

**Step 1: Update env var table**

Add all new `CETACEAN_AUTH_*` and `CETACEAN_TLS_*` variables to the table.

**Step 2: Update architecture section**

- Add `internal/auth/` package description
- Update middleware chain to include `auth`
- Note OIDC, Tailscale, cert, headers providers

**Step 3: Update key conventions**

- Note that `/-/*`, `/api*`, `/assets/*`, `/auth/*` routes are exempt from auth
- Note that `/auth/whoami` returns current identity

**Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with authentication architecture"
```

---

### Task 18: Integration test

**Files:**
- Create: `internal/auth/integration_test.go`

**Step 1: Write integration test**

Test the full middleware chain with each provider type:

```go
// internal/auth/integration_test.go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestFullChain_NoneMode(t *testing.T) {
	provider := &auth.NoneProvider{}
	mw := auth.Middleware(provider)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.IdentityFromContext(r.Context())
		if id == nil {
			t.Fatal("expected identity")
		}
		if id.Subject != "anonymous" {
			t.Errorf("subject=%q", id.Subject)
		}
		if id.Provider != "none" {
			t.Errorf("provider=%q", id.Provider)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(inner)

	// Authenticated routes should work
	for _, path := range []string{"/nodes", "/services", "/tasks/abc"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("%s: status=%d, want 200", path, w.Code)
		}
	}

	// Exempt routes should also work
	for _, path := range []string{"/-/health", "/api", "/assets/index.js"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("%s: status=%d, want 200", path, w.Code)
		}
	}
}
```

**Step 2: Run test**

Run: `go test ./internal/auth/ -v`
Expected: All PASS

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/auth/integration_test.go
git commit -m "test(auth): add integration tests for auth middleware chain"
```
