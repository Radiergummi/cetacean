# MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed an MCP server in Cetacean exposing cluster state as resources and write operations as tools, with OAuth 2.1 authorization and real-time notifications.

**Architecture:** New `internal/cluster` package extracts domain logic shared between REST and MCP. New `internal/mcp` package wires `mcp-go`'s `StreamableHTTPServer` to the cache and write client. New `internal/mcp/oauth` package implements OAuth 2.1 AS with CIMD. All mounted on the existing router behind the existing auth middleware.

**Tech Stack:** `github.com/mark3labs/mcp-go`, `crypto/hmac` (JWT), `html/template` (consent page)

---

### File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/config/mcp.go` | MCP config struct, env parsing, TOML support |
| Create | `internal/config/mcp_test.go` | Config parsing tests |
| Create | `internal/mcp/oauth/jwt.go` | JWT signing, verification, claims |
| Create | `internal/mcp/oauth/jwt_test.go` | JWT round-trip and validation tests |
| Create | `internal/mcp/oauth/cimd.go` | CIMD fetcher with SSRF protections |
| Create | `internal/mcp/oauth/cimd_test.go` | CIMD fetch, validation, and SSRF tests |
| Create | `internal/mcp/oauth/store.go` | In-memory auth code and refresh token stores |
| Create | `internal/mcp/oauth/store_test.go` | Store expiry, single-use, rotation tests |
| Create | `internal/mcp/oauth/server.go` | OAuth endpoints: authorize, token, revoke, metadata |
| Create | `internal/mcp/oauth/server_test.go` | OAuth flow integration tests |
| Create | `internal/mcp/oauth/consent.go` | Server-rendered HTML consent page |
| Create | `internal/mcp/oauth/consent_test.go` | Consent rendering and CSRF tests |
| Create | `internal/cluster/enrich.go` | EnrichedTask, secret redaction, cross-refs |
| Create | `internal/cluster/enrich_test.go` | Enrichment tests |
| Create | `internal/cluster/search.go` | Global search logic |
| Create | `internal/cluster/search_test.go` | Search tests |
| Create | `internal/cluster/state.go` | Service state derivation |
| Create | `internal/cluster/state_test.go` | State derivation tests |
| Create | `internal/mcp/server.go` | MCP server setup, session manager, mcp-go wiring |
| Create | `internal/mcp/server_test.go` | MCP server lifecycle tests |
| Create | `internal/mcp/resources.go` | MCP resource handlers |
| Create | `internal/mcp/resources_test.go` | Resource read/list tests |
| Create | `internal/mcp/tools.go` | MCP tool handlers |
| Create | `internal/mcp/tools_test.go` | Tool call tests |
| Create | `internal/mcp/notifications.go` | Cache event to MCP notification bridge |
| Create | `internal/mcp/notifications_test.go` | Notification filtering and dispatch tests |
| Modify | `internal/config/config.go` | Add MCP field to Config struct |
| Modify | `internal/config/file.go` | Add `[mcp]` TOML section |
| Modify | `internal/api/task_handlers.go` | Use `cluster.EnrichTask` |
| Modify | `internal/api/secret_handlers.go` | Use `cluster.RedactSecret` |
| Modify | `internal/api/search_handlers.go` | Use `cluster.Search`, `cluster.DeriveServiceState` |
| Modify | `internal/api/write_helpers.go` | Export `filterServiceRefs` for cluster pkg or move it |
| Modify | `internal/api/router.go` | Register MCP and OAuth endpoints |
| Modify | `main.go` | Wire MCP server when enabled |
| Modify | `go.mod` | Add `mark3labs/mcp-go` dependency |

---

### Task 1: MCP Configuration

**Files:**
- Create: `internal/config/mcp.go`
- Create: `internal/config/mcp_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/file.go`

- [ ] **Step 1: Write failing test for MCP config defaults**

```go
// internal/config/mcp_test.go
package config

import (
	"testing"
	"time"
)

func TestMCPConfigDefaults(t *testing.T) {
	cfg := DefaultMCPConfig()

	if cfg.Enabled {
		t.Error("MCP should be disabled by default")
	}
	if cfg.AccessTokenTTL != time.Hour {
		t.Errorf("access token TTL = %v, want 1h", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 720*time.Hour {
		t.Errorf("refresh token TTL = %v, want 720h", cfg.RefreshTokenTTL)
	}
	if cfg.SessionIdleTTL != 30*time.Minute {
		t.Errorf("session idle TTL = %v, want 30m", cfg.SessionIdleTTL)
	}
	if cfg.MaxSessions != 256 {
		t.Errorf("max sessions = %d, want 256", cfg.MaxSessions)
	}
	if cfg.OperationsLevel != nil {
		t.Error("operations level should be nil (inherits global)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestMCPConfigDefaults -v`
Expected: FAIL — `DefaultMCPConfig` not defined

- [ ] **Step 3: Implement MCP config struct and defaults**

```go
// internal/config/mcp.go
package config

import "time"

// MCPConfig holds configuration for the MCP server.
type MCPConfig struct {
	Enabled         bool
	SigningKey      string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	SessionIdleTTL  time.Duration
	MaxSessions     int
	OperationsLevel *OperationsLevel
}

// DefaultMCPConfig returns an MCPConfig with sensible defaults.
func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		Enabled:         false,
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 720 * time.Hour,
		SessionIdleTTL:  30 * time.Minute,
		MaxSessions:     256,
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestMCPConfigDefaults -v`
Expected: PASS

- [ ] **Step 5: Write failing test for env var parsing**

```go
// internal/config/mcp_test.go (append)

func TestMCPConfigFromEnv(t *testing.T) {
	t.Setenv("CETACEAN_MCP", "true")
	t.Setenv("CETACEAN_MCP_SIGNING_KEY", "test-secret")
	t.Setenv("CETACEAN_MCP_ACCESS_TOKEN_TTL", "2h")
	t.Setenv("CETACEAN_MCP_REFRESH_TOKEN_TTL", "48h")
	t.Setenv("CETACEAN_MCP_SESSION_IDLE_TTL", "15m")
	t.Setenv("CETACEAN_MCP_MAX_SESSIONS", "128")
	t.Setenv("CETACEAN_MCP_OPERATIONS_LEVEL", "2")

	cfg := LoadMCP()

	if !cfg.Enabled {
		t.Error("MCP should be enabled")
	}
	if cfg.SigningKey != "test-secret" {
		t.Errorf("signing key = %q, want %q", cfg.SigningKey, "test-secret")
	}
	if cfg.AccessTokenTTL != 2*time.Hour {
		t.Errorf("access token TTL = %v, want 2h", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 48*time.Hour {
		t.Errorf("refresh token TTL = %v, want 48h", cfg.RefreshTokenTTL)
	}
	if cfg.SessionIdleTTL != 15*time.Minute {
		t.Errorf("session idle TTL = %v, want 15m", cfg.SessionIdleTTL)
	}
	if cfg.MaxSessions != 128 {
		t.Errorf("max sessions = %d, want 128", cfg.MaxSessions)
	}
	if cfg.OperationsLevel == nil || *cfg.OperationsLevel != OpsConfiguration {
		t.Errorf("operations level = %v, want %v", cfg.OperationsLevel, OpsConfiguration)
	}
}
```

- [ ] **Step 6: Implement LoadMCP**

Add `LoadMCP()` to `internal/config/mcp.go` using the same `envOr*` helpers used by the existing `Load()` function. Parse each `CETACEAN_MCP_*` env var with fallback to defaults.

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestMCPConfig -v`
Expected: PASS

- [ ] **Step 8: Add MCP field to Config struct and TOML support**

In `internal/config/config.go`, add `MCP MCPConfig` to the `Config` struct.
In `internal/config/file.go`, add a `fileMCP` struct under the `[mcp]` TOML section, and merge it in `applyFile()`.

- [ ] **Step 9: Run all config tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/config/mcp.go internal/config/mcp_test.go internal/config/config.go internal/config/file.go
git commit -m "feat(config): add MCP server configuration"
```

---

### Task 2: JWT Token Library

**Files:**
- Create: `internal/mcp/oauth/jwt.go`
- Create: `internal/mcp/oauth/jwt_test.go`

- [ ] **Step 1: Write failing test for JWT sign and verify round-trip**

```go
// internal/mcp/oauth/jwt_test.go
package oauth

import (
	"testing"
	"time"
)

func TestJWTSignAndVerify(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte("test-secret-key-32-bytes-long!!!"),
		Issuer:    "https://cetacean.example.com",
		Audience:  "mcp",
	}

	claims := AccessTokenClaims{
		Subject: "user@example.com",
		Groups:  []string{"ops", "dev"},
	}

	token, err := issuer.IssueAccessToken(claims, time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}

	parsed, err := issuer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if parsed.Subject != "user@example.com" {
		t.Errorf("subject = %q, want %q", parsed.Subject, "user@example.com")
	}
	if len(parsed.Groups) != 2 || parsed.Groups[0] != "ops" {
		t.Errorf("groups = %v, want [ops dev]", parsed.Groups)
	}
}

func TestJWTExpiredToken(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte("test-secret-key-32-bytes-long!!!"),
		Issuer:    "https://cetacean.example.com",
		Audience:  "mcp",
	}

	token, err := issuer.IssueAccessToken(AccessTokenClaims{
		Subject: "user@example.com",
	}, -time.Hour) // already expired
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	_, err = issuer.VerifyAccessToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWTWrongSigningKey(t *testing.T) {
	issuer1 := &TokenIssuer{
		SigningKey: []byte("key-one-32-bytes-long-padding!!!"),
		Issuer:    "https://cetacean.example.com",
		Audience:  "mcp",
	}
	issuer2 := &TokenIssuer{
		SigningKey: []byte("key-two-32-bytes-long-padding!!!"),
		Issuer:    "https://cetacean.example.com",
		Audience:  "mcp",
	}

	token, _ := issuer1.IssueAccessToken(AccessTokenClaims{Subject: "user@example.com"}, time.Hour)

	_, err := issuer2.VerifyAccessToken(token)
	if err == nil {
		t.Fatal("expected error for wrong signing key")
	}
}

func TestJWTWrongAudience(t *testing.T) {
	issuer := &TokenIssuer{
		SigningKey: []byte("test-secret-key-32-bytes-long!!!"),
		Issuer:    "https://cetacean.example.com",
		Audience:  "mcp",
	}

	token, _ := issuer.IssueAccessToken(AccessTokenClaims{Subject: "user@example.com"}, time.Hour)

	other := &TokenIssuer{
		SigningKey: []byte("test-secret-key-32-bytes-long!!!"),
		Issuer:    "https://cetacean.example.com",
		Audience:  "wrong",
	}

	_, err := other.VerifyAccessToken(token)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/oauth/ -run TestJWT -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement TokenIssuer**

```go
// internal/mcp/oauth/jwt.go
package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"crypto/rand"
)

// AccessTokenClaims are the custom claims embedded in access tokens.
type AccessTokenClaims struct {
	Subject string   `json:"sub"`
	Groups  []string `json:"groups,omitempty"`
}

// jwtHeader is the fixed JOSE header for all tokens.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// jwtClaims is the full JWT payload.
type jwtClaims struct {
	Subject  string   `json:"sub"`
	Groups   []string `json:"groups,omitempty"`
	Issuer   string   `json:"iss"`
	Audience string   `json:"aud"`
	Expiry   int64    `json:"exp"`
	IssuedAt int64    `json:"iat"`
	ID       string   `json:"jti"`
}

// TokenIssuer signs and verifies JWTs.
type TokenIssuer struct {
	SigningKey []byte
	Issuer    string
	Audience  string
}

// IssueAccessToken creates a signed JWT with the given claims and TTL.
func (ti *TokenIssuer) IssueAccessToken(claims AccessTokenClaims, ttl time.Duration) (string, error) {
	now := time.Now()
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", fmt.Errorf("generate jti: %w", err)
	}

	payload := jwtClaims{
		Subject:  claims.Subject,
		Groups:   claims.Groups,
		Issuer:   ti.Issuer,
		Audience: ti.Audience,
		Expiry:   now.Add(ttl).Unix(),
		IssuedAt: now.Unix(),
		ID:       base64.RawURLEncoding.EncodeToString(jti),
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := jwtHeader + "." + payloadB64

	mac := hmac.New(sha256.New, ti.SigningKey)
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

// VerifyAccessToken verifies the JWT signature and claims.
func (ti *TokenIssuer) VerifyAccessToken(token string) (*AccessTokenClaims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}

	// Verify signature
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, ti.SigningKey)
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, errors.New("invalid signature")
	}

	// Decode claims
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Validate standard claims
	if claims.Issuer != ti.Issuer {
		return nil, fmt.Errorf("issuer mismatch: got %q, want %q", claims.Issuer, ti.Issuer)
	}
	if claims.Audience != ti.Audience {
		return nil, fmt.Errorf("audience mismatch: got %q, want %q", claims.Audience, ti.Audience)
	}
	if time.Now().Unix() > claims.Expiry {
		return nil, errors.New("token expired")
	}

	return &AccessTokenClaims{
		Subject: claims.Subject,
		Groups:  claims.Groups,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/oauth/ -run TestJWT -v`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/oauth/jwt.go internal/mcp/oauth/jwt_test.go
git commit -m "feat(mcp): add JWT token issuer and verifier"
```

---

### Task 3: CIMD Fetcher

**Files:**
- Create: `internal/mcp/oauth/cimd.go`
- Create: `internal/mcp/oauth/cimd_test.go`

- [ ] **Step 1: Write failing test for CIMD fetch and validation**

```go
// internal/mcp/oauth/cimd_test.go
package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCIMDFetchValid(t *testing.T) {
	meta := ClientMetadata{
		ClientID:     "", // set after server starts
		ClientName:   "Test Agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()

	meta.ClientID = srv.URL + "/client"
	// Update the server handler to return the correct client_id
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	})

	fetcher := &CIMDFetcher{Client: srv.Client()}
	result, err := fetcher.Fetch(context.Background(), meta.ClientID)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if result.ClientName != "Test Agent" {
		t.Errorf("client name = %q, want %q", result.ClientName, "Test Agent")
	}
}

func TestCIMDFetchClientIDMismatch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ClientMetadata{
			ClientID:   "https://wrong.example.com/client",
			ClientName: "Wrong",
		})
	}))
	defer srv.Close()

	fetcher := &CIMDFetcher{Client: srv.Client()}
	_, err := fetcher.Fetch(context.Background(), srv.URL+"/client")
	if err == nil {
		t.Fatal("expected error for client_id mismatch")
	}
}

func TestCIMDFetchResponseTooLarge(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 6*1024)) // 6KB, over 5KB limit
	}))
	defer srv.Close()

	fetcher := &CIMDFetcher{Client: srv.Client()}
	_, err := fetcher.Fetch(context.Background(), srv.URL+"/client")
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
}

func TestCIMDRejectsNonHTTPS(t *testing.T) {
	fetcher := &CIMDFetcher{}
	_, err := fetcher.Fetch(context.Background(), "http://example.com/client")
	if err == nil {
		t.Fatal("expected error for non-HTTPS URL")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/oauth/ -run TestCIMD -v`
Expected: FAIL — `CIMDFetcher` and `ClientMetadata` not defined

- [ ] **Step 3: Implement CIMDFetcher**

```go
// internal/mcp/oauth/cimd.go
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	cimdMaxResponseSize = 5 * 1024 // 5KB
	cimdFetchTimeout    = 5 * time.Second
	cimdCacheTTL        = time.Hour
)

// ClientMetadata represents an OAuth Client ID Metadata Document.
type ClientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

type cachedMetadata struct {
	metadata  *ClientMetadata
	fetchedAt time.Time
}

// CIMDFetcher fetches and validates OAuth Client ID Metadata Documents.
type CIMDFetcher struct {
	Client *http.Client // optional, defaults to http.DefaultClient

	mu    sync.RWMutex
	cache map[string]cachedMetadata
}

// Fetch retrieves and validates the CIMD at the given client_id URL.
func (f *CIMDFetcher) Fetch(ctx context.Context, clientID string) (*ClientMetadata, error) {
	// Validate URL scheme
	u, err := url.Parse(clientID)
	if err != nil {
		return nil, fmt.Errorf("invalid client_id URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, errors.New("client_id must use https scheme")
	}
	if u.Path == "" || u.Path == "/" {
		return nil, errors.New("client_id must contain a path component")
	}
	if u.Fragment != "" {
		return nil, errors.New("client_id must not contain a fragment")
	}
	if u.User != nil {
		return nil, errors.New("client_id must not contain credentials")
	}

	// Check cache
	if meta := f.getCached(clientID); meta != nil {
		return meta, nil
	}

	// SSRF: validate resolved IP is not private
	if err := validateResolvedIP(u.Hostname()); err != nil {
		return nil, fmt.Errorf("SSRF protection: %w", err)
	}

	// Fetch
	ctx, cancel := context.WithTimeout(ctx, cimdFetchTimeout)
	defer cancel()

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch CIMD: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CIMD returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, cimdMaxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("read CIMD: %w", err)
	}
	if len(body) > cimdMaxResponseSize {
		return nil, errors.New("CIMD response exceeds 5KB limit")
	}

	var meta ClientMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("decode CIMD: %w", err)
	}

	// Validate client_id matches
	if meta.ClientID != clientID {
		return nil, fmt.Errorf("client_id mismatch: document contains %q, expected %q", meta.ClientID, clientID)
	}

	// Reject symmetric auth methods
	switch meta.TokenEndpointAuthMethod {
	case "client_secret_post", "client_secret_basic":
		return nil, fmt.Errorf("symmetric auth method %q not allowed", meta.TokenEndpointAuthMethod)
	}

	f.putCached(clientID, &meta)
	return &meta, nil
}

// HasRedirectURI checks whether the given redirect URI is registered.
func (meta *ClientMetadata) HasRedirectURI(uri string) bool {
	for _, registered := range meta.RedirectURIs {
		if registered == uri {
			return true
		}
	}
	return false
}

func (f *CIMDFetcher) getCached(clientID string) *ClientMetadata {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.cache == nil {
		return nil
	}
	entry, ok := f.cache[clientID]
	if !ok || time.Since(entry.fetchedAt) > cimdCacheTTL {
		return nil
	}
	return entry.metadata
}

func (f *CIMDFetcher) putCached(clientID string, meta *ClientMetadata) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cache == nil {
		f.cache = make(map[string]cachedMetadata)
	}
	f.cache[clientID] = cachedMetadata{metadata: meta, fetchedAt: time.Now()}
}

// validateResolvedIP resolves the hostname and rejects private/reserved IPs.
func validateResolvedIP(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("resolved to private/reserved IP: %s", ip)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/oauth/ -run TestCIMD -v`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/oauth/cimd.go internal/mcp/oauth/cimd_test.go
git commit -m "feat(mcp): add CIMD fetcher with SSRF protections"
```

---

### Task 4: Authorization Code and Refresh Token Stores

**Files:**
- Create: `internal/mcp/oauth/store.go`
- Create: `internal/mcp/oauth/store_test.go`

- [ ] **Step 1: Write failing tests for auth code store**

```go
// internal/mcp/oauth/store_test.go
package oauth

import (
	"testing"
	"time"
)

func TestAuthCodeStoreRoundTrip(t *testing.T) {
	store := NewAuthCodeStore()

	code := store.Issue(AuthCodeData{
		ClientID:      "https://example.com/client",
		RedirectURI:   "http://localhost:8080/callback",
		CodeChallenge: "abc123",
		Subject:       "user@example.com",
		Groups:        []string{"ops"},
	}, 60*time.Second)

	data, ok := store.Redeem(code)
	if !ok {
		t.Fatal("code should be redeemable")
	}
	if data.Subject != "user@example.com" {
		t.Errorf("subject = %q, want %q", data.Subject, "user@example.com")
	}
}

func TestAuthCodeSingleUse(t *testing.T) {
	store := NewAuthCodeStore()
	code := store.Issue(AuthCodeData{Subject: "user@example.com"}, 60*time.Second)

	if _, ok := store.Redeem(code); !ok {
		t.Fatal("first redeem should succeed")
	}
	if _, ok := store.Redeem(code); ok {
		t.Fatal("second redeem should fail (single-use)")
	}
}

func TestAuthCodeExpiry(t *testing.T) {
	store := NewAuthCodeStore()
	code := store.Issue(AuthCodeData{Subject: "user@example.com"}, -time.Second) // already expired

	if _, ok := store.Redeem(code); ok {
		t.Fatal("expired code should not be redeemable")
	}
}
```

- [ ] **Step 2: Write failing tests for refresh token store**

```go
// internal/mcp/oauth/store_test.go (append)

func TestRefreshTokenStoreRoundTrip(t *testing.T) {
	store := NewRefreshTokenStore()

	token := store.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		Groups:   []string{"ops"},
		ClientID: "https://example.com/client",
	}, 720*time.Hour)

	data, ok := store.Validate(token)
	if !ok {
		t.Fatal("token should be valid")
	}
	if data.Subject != "user@example.com" {
		t.Errorf("subject = %q, want %q", data.Subject, "user@example.com")
	}
}

func TestRefreshTokenRotation(t *testing.T) {
	store := NewRefreshTokenStore()

	oldToken := store.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		ClientID: "https://example.com/client",
	}, 720*time.Hour)

	newToken, data, ok := store.Rotate(oldToken, 720*time.Hour)
	if !ok {
		t.Fatal("rotation should succeed")
	}
	if data.Subject != "user@example.com" {
		t.Errorf("subject = %q, want %q", data.Subject, "user@example.com")
	}

	// Old token is invalid
	if _, ok := store.Validate(oldToken); ok {
		t.Fatal("old token should be invalid after rotation")
	}

	// New token is valid
	if _, ok := store.Validate(newToken); !ok {
		t.Fatal("new token should be valid")
	}
}

func TestRefreshTokenTheftDetection(t *testing.T) {
	store := NewRefreshTokenStore()

	token := store.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		ClientID: "https://example.com/client",
	}, 720*time.Hour)

	// Rotate once (legitimate)
	newToken, _, ok := store.Rotate(token, 720*time.Hour)
	if !ok {
		t.Fatal("first rotation should succeed")
	}

	// Try to use old token again (theft attempt) — revokes entire grant
	if _, _, ok := store.Rotate(token, 720*time.Hour); ok {
		t.Fatal("reuse of rotated token should fail")
	}

	// New token should also be revoked
	if _, ok := store.Validate(newToken); ok {
		t.Fatal("new token should be revoked after theft detection")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/mcp/oauth/ -run TestAuthCode -v && go test ./internal/mcp/oauth/ -run TestRefreshToken -v`
Expected: FAIL — types not defined

- [ ] **Step 4: Implement auth code store**

```go
// internal/mcp/oauth/store.go
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

// AuthCodeData holds the data associated with an authorization code.
type AuthCodeData struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Subject       string
	Groups        []string
}

type authCodeEntry struct {
	data      AuthCodeData
	expiresAt time.Time
}

// AuthCodeStore is an in-memory store for authorization codes.
type AuthCodeStore struct {
	mu    sync.Mutex
	codes map[string]authCodeEntry
}

// NewAuthCodeStore creates a new authorization code store.
func NewAuthCodeStore() *AuthCodeStore {
	return &AuthCodeStore{codes: make(map[string]authCodeEntry)}
}

// Issue generates a new authorization code bound to the given data.
func (s *AuthCodeStore) Issue(data AuthCodeData, ttl time.Duration) string {
	code := generateOpaqueToken()
	hash := hashToken(code)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[hash] = authCodeEntry{data: data, expiresAt: time.Now().Add(ttl)}
	return code
}

// Redeem validates and consumes an authorization code (single-use).
func (s *AuthCodeStore) Redeem(code string) (AuthCodeData, bool) {
	hash := hashToken(code)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.codes[hash]
	if !ok {
		return AuthCodeData{}, false
	}
	delete(s.codes, hash) // single-use

	if time.Now().After(entry.expiresAt) {
		return AuthCodeData{}, false
	}

	return entry.data, true
}
```

- [ ] **Step 5: Implement refresh token store with theft detection**

```go
// internal/mcp/oauth/store.go (append)

// RefreshTokenData holds the data associated with a refresh token.
type RefreshTokenData struct {
	Subject  string
	Groups   []string
	ClientID string
	grantID  string // links token families for theft detection
}

type refreshTokenEntry struct {
	data      RefreshTokenData
	expiresAt time.Time
}

// RefreshTokenStore is an in-memory store for refresh tokens.
type RefreshTokenStore struct {
	mu     sync.Mutex
	tokens map[string]refreshTokenEntry
	grants map[string][]string // grantID -> list of token hashes in this family
}

// NewRefreshTokenStore creates a new refresh token store.
func NewRefreshTokenStore() *RefreshTokenStore {
	return &RefreshTokenStore{
		tokens: make(map[string]refreshTokenEntry),
		grants: make(map[string][]string),
	}
}

// Issue generates a new refresh token.
func (s *RefreshTokenStore) Issue(data RefreshTokenData, ttl time.Duration) string {
	token := generateOpaqueToken()
	hash := hashToken(token)
	grantID := generateOpaqueToken()
	data.grantID = grantID

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[hash] = refreshTokenEntry{data: data, expiresAt: time.Now().Add(ttl)}
	s.grants[grantID] = []string{hash}
	return token
}

// Validate checks that a refresh token is still valid.
func (s *RefreshTokenStore) Validate(token string) (RefreshTokenData, bool) {
	hash := hashToken(token)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tokens[hash]
	if !ok || time.Now().After(entry.expiresAt) {
		return RefreshTokenData{}, false
	}
	return entry.data, true
}

// Rotate consumes the old token and issues a new one. If the old token
// was already consumed (theft detection), the entire grant family is revoked.
func (s *RefreshTokenStore) Rotate(oldToken string, ttl time.Duration) (string, RefreshTokenData, bool) {
	oldHash := hashToken(oldToken)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tokens[oldHash]
	if !ok {
		// Token not found — might be a reuse attempt. We can't identify the
		// grant without the token, so just reject.
		return "", RefreshTokenData{}, false
	}

	if time.Now().After(entry.expiresAt) {
		delete(s.tokens, oldHash)
		return "", RefreshTokenData{}, false
	}

	// Consume old token
	delete(s.tokens, oldHash)

	// Issue new token in same grant family
	newToken := generateOpaqueToken()
	newHash := hashToken(newToken)
	entry.expiresAt = time.Now().Add(ttl)
	s.tokens[newHash] = entry
	s.grants[entry.data.grantID] = append(s.grants[entry.data.grantID], newHash)

	return newToken, entry.data, true
}

// RevokeGrant revokes all tokens in a grant family.
func (s *RefreshTokenStore) RevokeGrant(token string) {
	hash := hashToken(token)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tokens[hash]
	if !ok {
		return
	}

	for _, h := range s.grants[entry.data.grantID] {
		delete(s.tokens, h)
	}
	delete(s.grants, entry.data.grantID)
}

func generateOpaqueToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
```

- [ ] **Step 6: Fix theft detection test**

The current `Rotate` implementation consumes tokens on first use but doesn't detect reuse of already-consumed tokens across grant families. Update `Rotate` to track consumed token hashes: if a consumed hash is seen again, revoke the entire grant family via `grantID`.

Add a `consumed` map (`map[string]string`, hash → grantID) to `RefreshTokenStore`. On each rotation, add the old hash to `consumed`. In `Rotate`, check `consumed` first — if found, revoke the grant.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/mcp/oauth/ -run "TestAuthCode|TestRefreshToken" -v`
Expected: PASS (all 6 tests)

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/oauth/store.go internal/mcp/oauth/store_test.go
git commit -m "feat(mcp): add auth code and refresh token stores"
```

---

### Task 5: OAuth 2.1 Server Endpoints

**Files:**
- Create: `internal/mcp/oauth/server.go`
- Create: `internal/mcp/oauth/server_test.go`
- Create: `internal/mcp/oauth/consent.go`
- Create: `internal/mcp/oauth/consent_test.go`

- [ ] **Step 1: Write failing test for OAuth metadata endpoint**

```go
// internal/mcp/oauth/server_test.go
package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/config"
)

func TestMetadataEndpoint(t *testing.T) {
	srv := NewServer(ServerConfig{
		Issuer:   "https://cetacean.example.com",
		BasePath: "",
		MCP:      config.DefaultMCPConfig(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	srv.HandleMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var meta struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		RevocationEndpoint    string `json:"revocation_endpoint"`
		CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.Issuer != "https://cetacean.example.com" {
		t.Errorf("issuer = %q", meta.Issuer)
	}
	if meta.AuthorizationEndpoint != "https://cetacean.example.com/oauth/authorize" {
		t.Errorf("authorization_endpoint = %q", meta.AuthorizationEndpoint)
	}

	found := false
	for _, m := range meta.CodeChallengeMethodsSupported {
		if m == "S256" {
			found = true
		}
	}
	if !found {
		t.Error("S256 not in code_challenge_methods_supported")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/oauth/ -run TestMetadataEndpoint -v`
Expected: FAIL — `NewServer`, `ServerConfig` not defined

- [ ] **Step 3: Implement OAuth Server struct and metadata endpoint**

Create `internal/mcp/oauth/server.go` with:
- `ServerConfig` struct (Issuer, BasePath, MCPConfig, TokenIssuer, CIMDFetcher, AuthCodeStore, RefreshTokenStore)
- `Server` struct holding config and stores
- `NewServer(cfg)` constructor that initializes stores and token issuer
- `HandleMetadata(w, r)` returning RFC 8414 authorization server metadata as JSON
- `RegisterRoutes(mux)` to register all OAuth endpoints on the mux

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/oauth/ -run TestMetadataEndpoint -v`
Expected: PASS

- [ ] **Step 5: Write failing test for token exchange (code → access token)**

```go
// internal/mcp/oauth/server_test.go (append)

func TestTokenExchangeWithPKCE(t *testing.T) {
	srv := newTestServer(t)

	// Simulate an authorization code having been issued
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := computeS256Challenge(verifier)

	code := srv.authCodes.Issue(AuthCodeData{
		ClientID:      "https://example.com/client",
		RedirectURI:   "http://localhost:8080/callback",
		CodeChallenge: challenge,
		Subject:       "user@example.com",
		Groups:        []string{"ops"},
	}, 60*time.Second)

	// Exchange code for tokens
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {"https://example.com/client"},
		"code_verifier": {verifier},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.HandleToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("missing Cache-Control: no-store")
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tokenResp.AccessToken == "" {
		t.Error("access_token is empty")
	}
	if tokenResp.RefreshToken == "" {
		t.Error("refresh_token is empty")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", tokenResp.TokenType)
	}

	// Verify access token is valid
	claims, err := srv.tokenIssuer.VerifyAccessToken(tokenResp.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if claims.Subject != "user@example.com" {
		t.Errorf("subject = %q, want %q", claims.Subject, "user@example.com")
	}
}

func TestTokenExchangeWrongVerifier(t *testing.T) {
	srv := newTestServer(t)

	challenge := computeS256Challenge("correct-verifier-value-long-enough-43chars!!")
	code := srv.authCodes.Issue(AuthCodeData{
		ClientID:      "https://example.com/client",
		RedirectURI:   "http://localhost:8080/callback",
		CodeChallenge: challenge,
		Subject:       "user@example.com",
	}, 60*time.Second)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"client_id":     {"https://example.com/client"},
		"code_verifier": {"wrong-verifier-value-not-matching-the-chall!!"},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.HandleToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
```

Include a `newTestServer(t)` helper and `computeS256Challenge(verifier)` function.

- [ ] **Step 6: Implement token endpoint**

In `internal/mcp/oauth/server.go`, add `HandleToken(w, r)` supporting:
- `grant_type=authorization_code`: redeem code, verify PKCE (S256), issue access + refresh tokens
- `grant_type=refresh_token`: rotate refresh token, issue new access + refresh tokens
- Return 400 with `{"error": "..."}` on validation failures
- Set `Cache-Control: no-store` and `Pragma: no-cache` headers

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/mcp/oauth/ -run TestTokenExchange -v`
Expected: PASS

- [ ] **Step 8: Write failing test for revocation endpoint**

```go
// internal/mcp/oauth/server_test.go (append)

func TestRevocation(t *testing.T) {
	srv := newTestServer(t)

	refreshToken := srv.refreshTokens.Issue(RefreshTokenData{
		Subject:  "user@example.com",
		ClientID: "https://example.com/client",
	}, 720*time.Hour)

	form := url.Values{
		"token":           {refreshToken},
		"token_type_hint": {"refresh_token"},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.HandleRevoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Token should now be invalid
	if _, ok := srv.refreshTokens.Validate(refreshToken); ok {
		t.Fatal("revoked token should be invalid")
	}
}
```

- [ ] **Step 9: Implement revocation endpoint**

In `internal/mcp/oauth/server.go`, add `HandleRevoke(w, r)` per RFC 7009. Accept `token` and optional `token_type_hint`. Revoke the token's grant family. Always return 200 (per spec, even for invalid tokens).

- [ ] **Step 10: Run test to verify it passes**

Run: `go test ./internal/mcp/oauth/ -run TestRevocation -v`
Expected: PASS

- [ ] **Step 11: Implement authorization endpoint and consent page**

Create `internal/mcp/oauth/consent.go`:
- `consentTemplate` — an `html/template` rendering the consent page. Shows client name, logo (from CIMD), redirect URI, and approve/deny buttons.
- CSRF token generation and validation (bind to session cookie).
- Security headers: `Content-Security-Policy: frame-ancestors 'none'`, `X-Frame-Options: DENY`.

In `internal/mcp/oauth/server.go`, add `HandleAuthorize(w, r)`:
- `GET`: validate `response_type=code`, `client_id`, `redirect_uri`, `code_challenge`, `code_challenge_method`, `state`. Fetch CIMD if `client_id` is a URL. Render consent page.
- `POST`: validate CSRF token. If approved, generate auth code bound to (client_id, redirect_uri, code_challenge, identity). Redirect to `redirect_uri?code=...&state=...`. If denied, redirect with `error=access_denied`.
- On invalid `redirect_uri`: show error page, do NOT redirect.

- [ ] **Step 12: Write test for authorization flow**

```go
// internal/mcp/oauth/consent_test.go
package oauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsentPageRender(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/authorize?"+
		"response_type=code&"+
		"client_id=plain-client&"+
		"redirect_uri=http://localhost:8080/callback&"+
		"code_challenge=abc123&"+
		"code_challenge_method=S256&"+
		"state=xyz", nil)

	// Inject an authenticated identity into the context
	req = req.WithContext(authContextWithIdentity(req.Context(), "user@example.com", []string{"ops"}))

	srv.HandleAuthorize(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "plain-client") {
		t.Error("consent page should show client_id")
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options: DENY")
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("missing CSP frame-ancestors")
	}
}

func TestConsentPageRejectsInvalidRedirectURI(t *testing.T) {
	srv := newTestServer(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/oauth/authorize?"+
		"response_type=code&"+
		"client_id=plain-client&"+
		"redirect_uri=http://evil.example.com/steal&"+
		"code_challenge=abc123&"+
		"code_challenge_method=S256&"+
		"state=xyz", nil)
	req = req.WithContext(authContextWithIdentity(req.Context(), "user@example.com", nil))

	srv.HandleAuthorize(rec, req)

	// For plain client IDs (non-URL), redirect_uri validation is deferred.
	// For CIMD clients, the server verifies redirect_uri against the document.
	// Either way, it should not redirect to an unverified URI on error.
	if rec.Code == http.StatusFound {
		location := rec.Header().Get("Location")
		if strings.Contains(location, "evil.example.com") {
			t.Fatal("must not redirect to unverified redirect_uri")
		}
	}
}
```

- [ ] **Step 13: Run all OAuth tests**

Run: `go test ./internal/mcp/oauth/ -v`
Expected: PASS

- [ ] **Step 14: Implement RegisterRoutes**

```go
// In internal/mcp/oauth/server.go
func (s *Server) RegisterRoutes(mux *http.ServeMux, basePath string) {
	mux.HandleFunc("GET "+basePath+"/.well-known/oauth-authorization-server", s.HandleMetadata)
	mux.HandleFunc("GET "+basePath+"/oauth/authorize", s.HandleAuthorize)
	mux.HandleFunc("POST "+basePath+"/oauth/authorize", s.HandleAuthorize)
	mux.HandleFunc("POST "+basePath+"/oauth/token", s.HandleToken)
	mux.HandleFunc("POST "+basePath+"/oauth/revoke", s.HandleRevoke)
}
```

- [ ] **Step 15: Commit**

```bash
git add internal/mcp/oauth/server.go internal/mcp/oauth/server_test.go internal/mcp/oauth/consent.go internal/mcp/oauth/consent_test.go
git commit -m "feat(mcp): add OAuth 2.1 authorization server"
```

---

### Task 6: Extract Shared Domain Layer

**Files:**
- Create: `internal/cluster/enrich.go`
- Create: `internal/cluster/enrich_test.go`
- Create: `internal/cluster/search.go`
- Create: `internal/cluster/search_test.go`
- Create: `internal/cluster/state.go`
- Create: `internal/cluster/state_test.go`

- [ ] **Step 1: Write failing test for task enrichment**

```go
// internal/cluster/enrich_test.go
package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestEnrichTask(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
		},
	})
	c.SetNode(swarm.Node{
		ID: "node1",
		Description: swarm.NodeDescription{
			Hostname: "worker-1",
		},
	})

	task := swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
		NodeID:    "node1",
	}

	enriched := EnrichTask(c, task)
	if enriched.ServiceName != "web" {
		t.Errorf("ServiceName = %q, want %q", enriched.ServiceName, "web")
	}
	if enriched.NodeHostname != "worker-1" {
		t.Errorf("NodeHostname = %q, want %q", enriched.NodeHostname, "worker-1")
	}
}

func TestRedactSecret(t *testing.T) {
	secret := swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "my-secret"},
			Data:        []byte("sensitive-data"),
		},
	}

	redacted := RedactSecret(secret)
	if redacted.Spec.Data != nil {
		t.Error("secret data should be nil after redaction")
	}
	// Original should be unchanged
	if secret.Spec.Data == nil {
		t.Error("original secret data should not be modified")
	}
}

func TestRedactSecrets(t *testing.T) {
	secrets := []swarm.Secret{
		{Spec: swarm.SecretSpec{Data: []byte("a")}},
		{Spec: swarm.SecretSpec{Data: []byte("b")}},
	}

	redacted := RedactSecrets(secrets)
	for i, s := range redacted {
		if s.Spec.Data != nil {
			t.Errorf("secret[%d] data should be nil", i)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cluster/ -run "TestEnrichTask|TestRedactSecret" -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement enrichment functions**

```go
// internal/cluster/enrich.go
package cluster

import (
	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

// EnrichedTask extends swarm.Task with resolved service and node names.
type EnrichedTask struct {
	swarm.Task
	ServiceName  string `json:"ServiceName,omitempty"`
	NodeHostname string `json:"NodeHostname,omitempty"`
}

// EnrichTask resolves service name and node hostname for a task.
func EnrichTask(c *cache.Cache, t swarm.Task) EnrichedTask {
	et := EnrichedTask{Task: t}
	if svc, ok := c.GetService(t.ServiceID); ok {
		et.ServiceName = svc.Spec.Name
	}
	if node, ok := c.GetNode(t.NodeID); ok {
		et.NodeHostname = node.Description.Hostname
	}
	return et
}

// EnrichTasks enriches a slice of tasks.
func EnrichTasks(c *cache.Cache, tasks []swarm.Task) []EnrichedTask {
	result := make([]EnrichedTask, len(tasks))
	for i, t := range tasks {
		result[i] = EnrichTask(c, t)
	}
	return result
}

// RedactSecret returns a copy of the secret with Data set to nil.
func RedactSecret(s swarm.Secret) swarm.Secret {
	s.Spec.Data = nil
	return s
}

// RedactSecrets returns a new slice with all secret data redacted.
func RedactSecrets(secrets []swarm.Secret) []swarm.Secret {
	result := make([]swarm.Secret, len(secrets))
	for i, s := range secrets {
		result[i] = RedactSecret(s)
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cluster/ -run "TestEnrichTask|TestRedactSecret" -v`
Expected: PASS

- [ ] **Step 5: Write failing test for service state derivation**

```go
// internal/cluster/state_test.go
package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/swarm"
)

func TestDeriveServiceState(t *testing.T) {
	replicas := func(n uint64) *uint64 { return &n }

	tests := []struct {
		name         string
		mode         swarm.ServiceMode
		updateStatus *swarm.UpdateStatus
		running      int
		want         string
	}{
		{
			name:    "running normally",
			mode:    swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: replicas(3)}},
			running: 3,
			want:    "running",
		},
		{
			name:    "partially running",
			mode:    swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: replicas(3)}},
			running: 1,
			want:    "pending",
		},
		{
			name:    "no replicas running",
			mode:    swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: replicas(3)}},
			running: 0,
			want:    "failed",
		},
		{
			name:         "updating",
			mode:         swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: replicas(3)}},
			updateStatus: &swarm.UpdateStatus{State: swarm.UpdateStateUpdating},
			running:      3,
			want:         "updating",
		},
		{
			name:    "global with running tasks",
			mode:    swarm.ServiceMode{Global: &swarm.GlobalService{}},
			running: 3,
			want:    "running",
		},
		{
			name:    "global with no tasks",
			mode:    swarm.ServiceMode{Global: &swarm.GlobalService{}},
			running: 0,
			want:    "pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := swarm.Service{
				Spec:         swarm.ServiceSpec{Mode: tt.mode},
				UpdateStatus: tt.updateStatus,
			}
			got := DeriveServiceState(svc, tt.running)
			if got != tt.want {
				t.Errorf("DeriveServiceState() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 6: Implement service state derivation**

```go
// internal/cluster/state.go
package cluster

import "github.com/docker/docker/api/types/swarm"

// DeriveServiceState computes a human-readable state for a service.
func DeriveServiceState(svc swarm.Service, runningCount int) string {
	if svc.UpdateStatus != nil && svc.UpdateStatus.State == swarm.UpdateStateUpdating {
		return "updating"
	}

	if svc.Spec.Mode.Global != nil {
		if runningCount == 0 {
			return "pending"
		}
		return "running"
	}

	desired := 0
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		desired = int(*svc.Spec.Mode.Replicated.Replicas)
	}

	if desired > 0 && runningCount == 0 {
		return "failed"
	}
	if runningCount < desired {
		return "pending"
	}
	return "running"
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/cluster/ -run TestDeriveServiceState -v`
Expected: PASS

- [ ] **Step 8: Write failing test for search**

```go
// internal/cluster/search_test.go
package cluster

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestSearchByName(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web-frontend"}},
	})
	c.SetService(swarm.Service{
		ID:   "svc2",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api-backend"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "web-worker-1"},
	})

	results := Search(context.Background(), c, "web", 10)

	serviceResults := results["service"]
	if len(serviceResults) != 1 {
		t.Fatalf("service results = %d, want 1", len(serviceResults))
	}
	if serviceResults[0].Name != "web-frontend" {
		t.Errorf("service name = %q, want %q", serviceResults[0].Name, "web-frontend")
	}

	nodeResults := results["node"]
	if len(nodeResults) != 1 {
		t.Fatalf("node results = %d, want 1", len(nodeResults))
	}
}
```

- [ ] **Step 9: Implement search**

Create `internal/cluster/search.go` with:
- `SearchResult` struct: `Type`, `ID`, `Name`, `State` (optional)
- `Search(ctx, cache, query, limit)` returning `map[string][]SearchResult`
- Move `containsFold`, `segmentPrefixMatch`, `labelsMatch` from `internal/api/handlers.go` into this package (exported)
- Search across all 8 resource types concurrently (goroutines with `errgroup`)
- Secret data redacted in results (don't include raw data)

- [ ] **Step 10: Run test to verify it passes**

Run: `go test ./internal/cluster/ -run TestSearch -v`
Expected: PASS

- [ ] **Step 11: Run all cluster tests**

Run: `go test ./internal/cluster/ -v`
Expected: PASS

- [ ] **Step 12: Commit**

```bash
git add internal/cluster/
git commit -m "feat: add shared cluster domain layer"
```

---

### Task 7: Refactor REST Handlers to Use Shared Layer

**Files:**
- Modify: `internal/api/task_handlers.go`
- Modify: `internal/api/secret_handlers.go`
- Modify: `internal/api/search_handlers.go`
- Modify: `internal/api/handlers.go`

- [ ] **Step 1: Update task handlers to use cluster.EnrichTask**

In `internal/api/task_handlers.go`:
- Remove the local `EnrichedTask` struct, `enrichTask()`, and `enrichTasks()` functions.
- Replace all usages with `cluster.EnrichTask(h.cache, task)` and `cluster.EnrichTasks(h.cache, tasks)`.
- Import `github.com/radiergummi/cetacean/internal/cluster`.

- [ ] **Step 2: Run task handler tests**

Run: `go test ./internal/api/ -run Task -v`
Expected: PASS

- [ ] **Step 3: Update secret handlers to use cluster.RedactSecret**

In `internal/api/secret_handlers.go`:
- Replace inline `sec.Spec.Data = nil` with `sec = cluster.RedactSecret(sec)` in the detail handler.
- Replace the `prepare` function's inline loop with `cluster.RedactSecrets(secrets)` in the list handler.
- Do the same in `internal/api/write_secret_handlers.go` for the create handler.

- [ ] **Step 4: Run secret handler tests**

Run: `go test ./internal/api/ -run Secret -v`
Expected: PASS

- [ ] **Step 5: Update search handler to use cluster.Search and cluster.DeriveServiceState**

In `internal/api/search_handlers.go`:
- Replace the inline service state derivation with `cluster.DeriveServiceState(svc, running)`.
- Move the `containsFold`, `segmentPrefixMatch`, and `labelsMatch` helpers from `handlers.go` to `internal/cluster/search.go` (already done in Task 6). Update imports in `handlers.go` to call `cluster.ContainsFold`, etc.
- The `HandleSearch` handler itself stays in `api/` because it handles HTTP concerns (query params, response formatting, ACL filtering). It calls `cluster` functions for the matching logic.

- [ ] **Step 6: Run search handler tests**

Run: `go test ./internal/api/ -run Search -v`
Expected: PASS

- [ ] **Step 7: Run full test suite to verify no regressions**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/api/ internal/cluster/
git commit -m "refactor: use shared cluster layer in REST handlers"
```

---

### Task 8: MCP Server Core and Router Integration

**Files:**
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/server_test.go`
- Modify: `internal/api/router.go`
- Modify: `main.go`
- Modify: `go.mod`

- [ ] **Step 1: Add mcp-go dependency**

```bash
go get github.com/mark3labs/mcp-go
```

- [ ] **Step 2: Write failing test for MCP server creation**

```go
// internal/mcp/server_test.go
package mcp

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestNewServer(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true

	srv, err := New(c, nil, nil, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv == nil {
		t.Fatal("server is nil")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestNewServer -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 4: Implement MCP server core**

```go
// internal/mcp/server.go
package mcp

import (
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// Server is the Cetacean MCP server.
type Server struct {
	cache           *cache.Cache
	writeClient     DockerWriteClient
	acl             *acl.Evaluator
	config          config.MCPConfig
	mcpServer       *mcpserver.MCPServer
	httpServer      *mcpserver.StreamableHTTPServer
}

// DockerWriteClient defines the write operations available to MCP tools.
// Mirrors the interface from internal/api/handlers.go.
type DockerWriteClient interface {
	// Include all methods from the composed write interfaces
	// that the MCP tools need access to.
}

// New creates a new MCP server.
func New(
	c *cache.Cache,
	writeClient DockerWriteClient,
	aclEval *acl.Evaluator,
	cfg config.MCPConfig,
) (*Server, error) {
	srv := &Server{
		cache:       c,
		writeClient: writeClient,
		acl:         aclEval,
		config:      cfg,
	}

	mcpSrv := mcpserver.NewMCPServer(
		"Cetacean",
		"1.0.0",
		mcpserver.WithResourceCapabilities(true, true), // subscribe + listChanged
		mcpserver.WithToolCapabilities(true),
	)

	srv.mcpServer = mcpSrv
	srv.registerResources()
	srv.registerTools()

	httpSrv := mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithStateful(true),
		mcpserver.WithSessionIdleTTL(cfg.SessionIdleTTL),
		mcpserver.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			// Bridge auth identity from HTTP context to MCP context
			if id := auth.IdentityFromContext(r.Context()); id != nil {
				ctx = auth.ContextWithIdentity(ctx, id)
			}
			return ctx
		}),
	)

	srv.httpServer = httpSrv
	return srv, nil
}

// Handler returns the http.Handler for the MCP endpoint.
func (s *Server) Handler() http.Handler {
	return s.httpServer
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/mcp/ -run TestNewServer -v`
Expected: PASS

- [ ] **Step 6: Add placeholder registerResources and registerTools**

Add empty methods `registerResources()` and `registerTools()` to `Server` so the code compiles. These are implemented in Tasks 9 and 10.

- [ ] **Step 7: Wire MCP into router**

In `internal/api/router.go`, add to `RouterConfig`:
```go
MCPServer *mcp.Server // nil if MCP disabled
```

In `NewRouter`, before the SPA fallback registration:
```go
if cfg.MCPServer != nil {
	mux.Handle(cfg.BasePath+"/mcp", cfg.MCPServer.Handler())
}
```

In `main.go`, conditionally create the MCP server when `cfg.MCP.Enabled` and pass it to `RouterConfig`.

- [ ] **Step 8: Wire OAuth routes into router**

In `main.go`, conditionally register OAuth routes when MCP is enabled and auth provider is not nil:
```go
if cfg.MCP.Enabled && authProvider != nil {
	oauthServer := oauth.NewServer(oauth.ServerConfig{...})
	oauthServer.RegisterRoutes(mux, cfg.BasePath)
}
```

- [ ] **Step 9: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/mcp/server.go internal/mcp/server_test.go internal/api/router.go main.go go.mod go.sum
git commit -m "feat(mcp): add MCP server core and router integration"
```

---

### Task 9: MCP Resources

**Files:**
- Create: `internal/mcp/resources.go`
- Create: `internal/mcp/resources_test.go`

- [ ] **Step 1: Write failing test for resource template registration**

```go
// internal/mcp/resources_test.go
package mcp

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestReadServiceResource(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
		},
	})

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, _ := New(c, nil, nil, cfg)

	// Simulate a resources/read for cetacean://services/svc1
	content, err := srv.readResource(context.Background(), "cetacean://services/svc1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if content == "" {
		t.Fatal("content is empty")
	}
}

func TestReadNodeResource(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node1",
		Description: swarm.NodeDescription{Hostname: "manager-1"},
	})

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, _ := New(c, nil, nil, cfg)

	content, err := srv.readResource(context.Background(), "cetacean://nodes/node1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if content == "" {
		t.Fatal("content is empty")
	}
}

func TestReadSecretResourceRedactsData(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "db-password"},
			Data:        []byte("super-secret"),
		},
	})

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, _ := New(c, nil, nil, cfg)

	content, err := srv.readResource(context.Background(), "cetacean://secrets/sec1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}
	if strings.Contains(content, "super-secret") {
		t.Error("secret data should be redacted")
	}
}

func TestReadNonexistentResource(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, _ := New(c, nil, nil, cfg)

	_, err := srv.readResource(context.Background(), "cetacean://services/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent resource")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestRead -v`
Expected: FAIL — `readResource` not defined

- [ ] **Step 3: Implement resource registration and read handlers**

```go
// internal/mcp/resources.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/radiergummi/cetacean/internal/cluster"
)

func (s *Server) registerResources() {
	// Static resources
	s.mcpServer.AddResource(mcp.Resource{
		URI:         "cetacean://cluster",
		Name:        "Cluster",
		Description: "Swarm cluster info",
		MIMEType:    "application/json",
	}, s.handleReadResource)

	s.mcpServer.AddResource(mcp.Resource{
		URI:         "cetacean://recommendations",
		Name:        "Recommendations",
		Description: "Current cluster recommendations",
		MIMEType:    "application/json",
	}, s.handleReadResource)

	s.mcpServer.AddResource(mcp.Resource{
		URI:         "cetacean://history",
		Name:        "History",
		Description: "Recent change history",
		MIMEType:    "application/json",
	}, s.handleReadResource)

	// Resource templates
	templates := []mcp.ResourceTemplate{
		{URITemplate: "cetacean://nodes/{id}", Name: "Node", Description: "Node detail", MIMEType: "application/json"},
		{URITemplate: "cetacean://services/{id}", Name: "Service", Description: "Service detail", MIMEType: "application/json"},
		{URITemplate: "cetacean://services/{id}/logs", Name: "Service Logs", Description: "Service log stream", MIMEType: "application/json"},
		{URITemplate: "cetacean://tasks/{id}", Name: "Task", Description: "Task detail", MIMEType: "application/json"},
		{URITemplate: "cetacean://stacks/{name}", Name: "Stack", Description: "Stack detail", MIMEType: "application/json"},
		{URITemplate: "cetacean://configs/{id}", Name: "Config", Description: "Config detail", MIMEType: "application/json"},
		{URITemplate: "cetacean://secrets/{id}", Name: "Secret", Description: "Secret metadata (data redacted)", MIMEType: "application/json"},
		{URITemplate: "cetacean://networks/{id}", Name: "Network", Description: "Network detail", MIMEType: "application/json"},
		{URITemplate: "cetacean://volumes/{name}", Name: "Volume", Description: "Volume detail", MIMEType: "application/json"},
	}
	for _, tmpl := range templates {
		s.mcpServer.AddResourceTemplate(tmpl, s.handleReadResource)
	}
}

func (s *Server) handleReadResource(ctx context.Context, uri string) (string, error) {
	return s.readResource(ctx, uri)
}

func (s *Server) readResource(ctx context.Context, uri string) (string, error) {
	// Parse URI: cetacean://type/id
	path := strings.TrimPrefix(uri, "cetacean://")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) == 0 {
		return "", fmt.Errorf("invalid resource URI: %s", uri)
	}

	resourceType := parts[0]
	resourceID := ""
	if len(parts) > 1 {
		resourceID = parts[1]
	}
	subResource := ""
	if len(parts) > 2 {
		subResource = parts[2]
	}

	// TODO: ACL check using identity from context

	var data any
	var found bool

	switch resourceType {
	case "nodes":
		if resourceID == "" {
			data = s.cache.ListNodes()
			found = true
		} else {
			data, found = s.cache.GetNode(resourceID)
		}
	case "services":
		if resourceID == "" {
			data = s.cache.ListServices()
			found = true
		} else if subResource == "logs" {
			return s.readServiceLogs(ctx, resourceID)
		} else {
			data, found = s.cache.GetService(resourceID)
		}
	case "tasks":
		if resourceID == "" {
			tasks := s.cache.ListTasks()
			data = cluster.EnrichTasks(s.cache, tasks)
			found = true
		} else {
			task, ok := s.cache.GetTask(resourceID)
			if ok {
				data = cluster.EnrichTask(s.cache, task)
				found = true
			}
		}
	case "stacks":
		if resourceID == "" {
			data = s.cache.ListStacks()
			found = true
		} else {
			data, found = s.cache.GetStackDetail(resourceID)
		}
	case "configs":
		if resourceID == "" {
			data = s.cache.ListConfigs()
			found = true
		} else {
			data, found = s.cache.GetConfig(resourceID)
		}
	case "secrets":
		if resourceID == "" {
			data = cluster.RedactSecrets(s.cache.ListSecrets())
			found = true
		} else {
			sec, ok := s.cache.GetSecret(resourceID)
			if ok {
				data = cluster.RedactSecret(sec)
				found = true
			}
		}
	case "networks":
		if resourceID == "" {
			data = s.cache.ListNetworks()
			found = true
		} else {
			data, found = s.cache.GetNetwork(resourceID)
		}
	case "volumes":
		if resourceID == "" {
			data = s.cache.ListVolumes()
			found = true
		} else {
			data, found = s.cache.GetVolume(resourceID)
		}
	case "cluster":
		data = s.cache.Snapshot()
		found = true
	case "recommendations":
		if s.recEngine != nil {
			data = s.recEngine.Results()
		} else {
			data = []any{}
		}
		found = true
	case "history":
		data = s.cache.History().Recent(100)
		found = true
	default:
		return "", fmt.Errorf("unknown resource type: %s", resourceType)
	}

	if !found {
		return "", fmt.Errorf("resource not found: %s", uri)
	}

	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal resource: %w", err)
	}
	return string(b), nil
}

func (s *Server) readServiceLogs(ctx context.Context, serviceID string) (string, error) {
	// TODO: implement log reading with cursor support
	return "[]", nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestRead -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/resources.go internal/mcp/resources_test.go
git commit -m "feat(mcp): add resource handlers"
```

---

### Task 10: MCP Tools

**Files:**
- Create: `internal/mcp/tools.go`
- Create: `internal/mcp/tools_test.go`

- [ ] **Step 1: Write failing test for scale_service tool**

```go
// internal/mcp/tools_test.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

type mockServiceLifecycle struct {
	scaleServiceFn func(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
}

func (m *mockServiceLifecycle) ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
	if m.scaleServiceFn != nil {
		return m.scaleServiceFn(ctx, id, replicas)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

// Add stub methods for other ServiceLifecycleWriter methods...

func TestScaleServiceTool(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{}},
		},
	})

	var calledWith uint64
	mock := &mockServiceLifecycle{
		scaleServiceFn: func(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
			calledWith = replicas
			return swarm.Service{}, nil
		},
	}

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	opsLevel := config.OpsOperational
	cfg.OperationsLevel = &opsLevel
	srv, _ := newTestMCPServer(c, mock, nil, cfg)

	args := map[string]any{"id": "svc1", "replicas": float64(5)}
	result, err := srv.callTool(context.Background(), "scale_service", args)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if calledWith != 5 {
		t.Errorf("scaled to %d, want 5", calledWith)
	}
	if result == "" {
		t.Error("result is empty")
	}
}

func TestScaleServiceToolDeniedByTier(t *testing.T) {
	c := cache.New(nil)
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	opsLevel := config.OpsReadOnly
	cfg.OperationsLevel = &opsLevel
	srv, _ := newTestMCPServer(c, nil, nil, cfg)

	args := map[string]any{"id": "svc1", "replicas": float64(5)}
	_, err := srv.callTool(context.Background(), "scale_service", args)
	if err == nil {
		t.Fatal("expected error for read-only operations level")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestScaleService -v`
Expected: FAIL — `callTool` not defined

- [ ] **Step 3: Implement tool registration and dispatch**

```go
// internal/mcp/tools.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

type toolDef struct {
	tool    mcplib.Tool
	tier    config.OperationsLevel
	handler func(ctx context.Context, args map[string]any) (string, error)
}

func (s *Server) registerTools() {
	tools := []toolDef{
		// Parameterized reads (no tier gating)
		{
			tool: mcplib.NewTool("get_logs",
				mcplib.WithDescription("Get recent log lines for a service"),
				mcplib.WithString("service", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithNumber("tail", mcplib.Description("Number of lines (default 100)")),
				mcplib.WithString("since", mcplib.Description("Start time (RFC3339)")),
				mcplib.WithString("level", mcplib.Description("Minimum log level")),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetLogs,
		},
		{
			tool: mcplib.NewTool("search",
				mcplib.WithDescription("Search across all cluster resources"),
				mcplib.WithString("query", mcplib.Required(), mcplib.Description("Search query")),
				mcplib.WithString("types", mcplib.Description("Comma-separated resource types to search")),
				mcplib.WithNumber("limit", mcplib.Description("Max results per type (default 3)")),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolSearch,
		},
		// Tier 1 — Operational
		{
			tool: mcplib.NewTool("scale_service",
				mcplib.WithDescription("Scale a service to a specific replica count"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithNumber("replicas", mcplib.Required(), mcplib.Description("Target replica count")),
			),
			tier:    config.OpsOperational,
			handler: s.toolScaleService,
		},
		// ... register all other tools following the same pattern
	}

	opsLevel := s.effectiveOperationsLevel()

	for _, td := range tools {
		if opsLevel < td.tier {
			continue // skip tools above the configured tier
		}

		handler := td.handler
		s.mcpServer.AddTool(td.tool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			result, err := handler(ctx, req.Params.Arguments)
			if err != nil {
				return mcplib.NewToolResultError(err.Error()), nil
			}
			return mcplib.NewToolResultText(result), nil
		})
	}
}

func (s *Server) effectiveOperationsLevel() config.OperationsLevel {
	if s.config.OperationsLevel != nil {
		return *s.config.OperationsLevel
	}
	return s.globalOpsLevel
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (string, error) {
	// Internal dispatch for testing. In production, mcp-go handles dispatch.
	// This method looks up the registered handler by name and calls it directly.
	// Implementation: iterate s.tools map, find by name, call handler.
	return "", fmt.Errorf("not implemented")
}

func (s *Server) toolScaleService(ctx context.Context, args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	replicasF, _ := args["replicas"].(float64)
	replicas := uint64(replicasF)

	// ACL check
	if id := auth.IdentityFromContext(ctx); s.acl != nil && id != nil {
		if svc, ok := s.cache.GetService(id); ok {
			if !s.acl.Can(id, "write", "service:"+svc.Spec.Name) {
				return "", fmt.Errorf("write access denied for service:%s", svc.Spec.Name)
			}
		}
	}

	result, err := s.serviceLifecycle.ScaleService(ctx, id, replicas)
	if err != nil {
		return "", err
	}

	b, _ := json.Marshal(result)
	return string(b), nil
}

func (s *Server) toolGetLogs(ctx context.Context, args map[string]any) (string, error) {
	// TODO: implement log retrieval
	return "[]", nil
}

func (s *Server) toolSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	limit := 3
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	results := cluster.Search(ctx, s.cache, query, limit)
	b, _ := json.Marshal(results)
	return string(b), nil
}
```

Implement all remaining tool handlers following the same pattern: extract args, ACL check, call write client, marshal result.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestScaleService -v`
Expected: PASS

- [ ] **Step 5: Write tests for remaining tier 1 and tier 2 tools**

Add tests for `update_service_image`, `rollback_service`, `restart_service`, `remove_task`, `update_service_env`, etc. Follow the same mock pattern as `TestScaleServiceTool`. Each test verifies:
- Correct write client method is called with correct args
- ACL denial returns error
- Tier denial returns error

- [ ] **Step 6: Implement remaining tool handlers**

Add handler methods for all tools listed in the design spec. Each follows the same pattern: extract args → ACL check → call write client → marshal response.

- [ ] **Step 7: Run all tool tests**

Run: `go test ./internal/mcp/ -run TestTool -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/tools_test.go
git commit -m "feat(mcp): add tool handlers for write operations"
```

---

### Task 11: Notifications and Subscriptions

**Files:**
- Create: `internal/mcp/notifications.go`
- Create: `internal/mcp/notifications_test.go`

- [ ] **Step 1: Write failing test for notification dispatch**

```go
// internal/mcp/notifications_test.go
package mcp

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestNotificationMatchesSubscription(t *testing.T) {
	nm := &NotificationManager{}

	nm.Subscribe("session1", "cetacean://services/svc1")
	nm.Subscribe("session1", "cetacean://nodes/node1")

	event := cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc1",
		Name:   "web",
	}

	uris := nm.MatchingURIs("session1", event)
	if len(uris) != 1 || uris[0] != "cetacean://services/svc1" {
		t.Errorf("matching URIs = %v, want [cetacean://services/svc1]", uris)
	}
}

func TestNotificationNoMatchForUnsubscribed(t *testing.T) {
	nm := &NotificationManager{}

	nm.Subscribe("session1", "cetacean://services/svc1")

	event := cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc2", // different service
		Name:   "api",
	}

	uris := nm.MatchingURIs("session1", event)
	if len(uris) != 0 {
		t.Errorf("matching URIs = %v, want empty", uris)
	}
}

func TestUnsubscribe(t *testing.T) {
	nm := &NotificationManager{}

	nm.Subscribe("session1", "cetacean://services/svc1")
	nm.Unsubscribe("session1", "cetacean://services/svc1")

	event := cache.Event{
		Type:   cache.EventService,
		Action: "update",
		ID:     "svc1",
	}

	uris := nm.MatchingURIs("session1", event)
	if len(uris) != 0 {
		t.Errorf("matching URIs = %v, want empty after unsubscribe", uris)
	}
}

func TestListChangedDetection(t *testing.T) {
	nm := &NotificationManager{}

	event := cache.Event{
		Type:   cache.EventService,
		Action: "create",
		ID:     "svc-new",
	}

	if !nm.IsListChange(event) {
		t.Error("create action should be a list change")
	}

	event.Action = "remove"
	if !nm.IsListChange(event) {
		t.Error("remove action should be a list change")
	}

	event.Action = "update"
	if nm.IsListChange(event) {
		t.Error("update action should not be a list change")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run TestNotification -v`
Expected: FAIL — `NotificationManager` not defined

- [ ] **Step 3: Implement NotificationManager**

```go
// internal/mcp/notifications.go
package mcp

import (
	"strings"
	"sync"

	"github.com/radiergummi/cetacean/internal/cache"
)

// NotificationManager tracks per-session resource subscriptions and
// matches cache events to subscribed URIs.
type NotificationManager struct {
	mu            sync.RWMutex
	subscriptions map[string]map[string]struct{} // sessionID -> set of URIs
}

// Subscribe registers a resource URI subscription for a session.
func (nm *NotificationManager) Subscribe(sessionID, uri string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if nm.subscriptions == nil {
		nm.subscriptions = make(map[string]map[string]struct{})
	}
	if nm.subscriptions[sessionID] == nil {
		nm.subscriptions[sessionID] = make(map[string]struct{})
	}
	nm.subscriptions[sessionID][uri] = struct{}{}
}

// Unsubscribe removes a resource URI subscription for a session.
func (nm *NotificationManager) Unsubscribe(sessionID, uri string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if nm.subscriptions[sessionID] != nil {
		delete(nm.subscriptions[sessionID], uri)
	}
}

// RemoveSession removes all subscriptions for a session.
func (nm *NotificationManager) RemoveSession(sessionID string) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	delete(nm.subscriptions, sessionID)
}

// MatchingURIs returns the subscribed URIs that match a cache event.
func (nm *NotificationManager) MatchingURIs(sessionID string, event cache.Event) []string {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	subs := nm.subscriptions[sessionID]
	if len(subs) == 0 {
		return nil
	}

	prefix := eventTypeToURIPrefix(event.Type)
	if prefix == "" {
		return nil
	}

	// Check for exact match: cetacean://type/id
	candidate := prefix + event.ID
	var matches []string
	if _, ok := subs[candidate]; ok {
		matches = append(matches, candidate)
	}

	// Check for log subscriptions: cetacean://services/id/logs
	if event.Type == cache.EventService {
		logURI := prefix + event.ID + "/logs"
		if _, ok := subs[logURI]; ok {
			matches = append(matches, logURI)
		}
	}

	return matches
}

// IsListChange returns true if the event represents a resource being
// created or removed (as opposed to updated).
func (nm *NotificationManager) IsListChange(event cache.Event) bool {
	return event.Action == "create" || event.Action == "remove"
}

func eventTypeToURIPrefix(t cache.EventType) string {
	switch t {
	case cache.EventNode:
		return "cetacean://nodes/"
	case cache.EventService:
		return "cetacean://services/"
	case cache.EventTask:
		return "cetacean://tasks/"
	case cache.EventConfig:
		return "cetacean://configs/"
	case cache.EventSecret:
		return "cetacean://secrets/"
	case cache.EventNetwork:
		return "cetacean://networks/"
	case cache.EventVolume:
		return "cetacean://volumes/"
	case cache.EventStack:
		return "cetacean://stacks/"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mcp/ -run TestNotification -v`
Expected: PASS

- [ ] **Step 5: Wire notifications to cache events**

In `internal/mcp/server.go`, add a method to start listening for cache events:

```go
// StartNotifications registers the MCP server as a cache event listener.
// Call this after the cache's primary OnChange is set (so it chains, not replaces).
func (s *Server) StartNotifications() {
	s.cache.AddOnChangeListener(func(event cache.Event) {
		// For each active session, check subscriptions and send notifications
		s.notifications.dispatch(event, s.mcpServer, s.acl)
	})
}
```

Add a `dispatch` method to `NotificationManager` that:
1. Iterates all sessions
2. For each session, calls `MatchingURIs` to find matching subscriptions
3. ACL-checks the identity bound to the session
4. Sends `notifications/resources/updated` via the session's notification channel
5. If `IsListChange`, sends `notifications/resources/list_changed` to all eligible sessions

**Note:** The cache currently supports a single `OnChangeFunc`. If `AddOnChangeListener` doesn't exist, add a fan-out wrapper: the primary callback calls the broadcaster AND the MCP notification manager. Alternatively, check if the cache already supports multiple listeners.

- [ ] **Step 6: Run all notification tests**

Run: `go test ./internal/mcp/ -run TestNotification -v`
Expected: PASS

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/notifications.go internal/mcp/notifications_test.go internal/mcp/server.go
git commit -m "feat(mcp): add subscription notifications"
```

---

### Task 12: Log Resource and Tool

**Files:**
- Modify: `internal/mcp/resources.go`
- Modify: `internal/mcp/tools.go`
- Create: `internal/mcp/logs.go`
- Create: `internal/mcp/logs_test.go`

- [ ] **Step 1: Write failing test for log resource read with cursor**

```go
// internal/mcp/logs_test.go
package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestReadLogsResource(t *testing.T) {
	// This test requires a mock DockerLogStreamer.
	// The log resource should return recent lines and a cursor for pagination.
	c, mock := newTestCacheWithLogs(t, "svc1", []string{
		"2026-04-11T10:00:00Z line 1",
		"2026-04-11T10:00:01Z line 2",
		"2026-04-11T10:00:02Z line 3",
	})

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, _ := newTestMCPServerWithLogs(c, mock, cfg)

	content, err := srv.readResource(context.Background(), "cetacean://services/svc1/logs")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var result LogResourceResponse
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Lines) != 3 {
		t.Errorf("lines = %d, want 3", len(result.Lines))
	}
	if result.Cursor == "" {
		t.Error("cursor should be set for pagination")
	}
}
```

- [ ] **Step 2: Implement log reading**

Create `internal/mcp/logs.go` with:
- `LogResourceResponse` struct: `Lines []string`, `Cursor string`
- `readServiceLogs(ctx, serviceID)` that calls the `DockerLogStreamer` interface to get recent lines
- Cursor-based pagination: encode the timestamp of the last line as an opaque cursor. On subsequent reads (with cursor in session state), return only lines after that timestamp.

- [ ] **Step 3: Implement get_logs tool**

Complete the `toolGetLogs` handler in `internal/mcp/tools.go`:
- Accept `service`, `tail` (default 100), `since` (RFC3339), `level` parameters
- Call `DockerLogStreamer` to get logs
- Return formatted log lines as text

- [ ] **Step 4: Run log tests**

Run: `go test ./internal/mcp/ -run TestLog -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/logs.go internal/mcp/logs_test.go internal/mcp/resources.go internal/mcp/tools.go
git commit -m "feat(mcp): add log resource and tool"
```

---

### Task 13: End-to-End Integration Test

**Files:**
- Create: `internal/mcp/integration_test.go`

- [ ] **Step 1: Write integration test for full MCP flow**

```go
// internal/mcp/integration_test.go
package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestMCPEndToEnd(t *testing.T) {
	// Set up cache with test data
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Description: swarm.NodeDescription{Hostname: "manager-1"},
	})

	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, err := New(c, nil, nil, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	handler := srv.Handler()
	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Send initialize request
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`

	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(initReq))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d", resp.StatusCode)
	}

	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("no Mcp-Session-Id in response")
	}

	// Send resources/list request
	listReq := `{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}`
	req, _ := http.NewRequest("POST", ts.URL, strings.NewReader(listReq))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", sessionID)

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("resources/list: %v", err)
	}
	defer resp2.Body.Close()

	var listResp struct {
		Result struct {
			Resources []struct {
				URI  string `json:"uri"`
				Name string `json:"name"`
			} `json:"resources"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(listResp.Result.Resources) == 0 {
		t.Error("expected at least one resource in list")
	}
}
```

- [ ] **Step 2: Run integration test**

Run: `go test ./internal/mcp/ -run TestMCPEndToEnd -v`
Expected: PASS

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/integration_test.go
git commit -m "test(mcp): add end-to-end integration test"
```

---

### Task 14: Documentation and Configuration Reference

**Files:**
- Modify: `CLAUDE.md` — add MCP environment variables to the table
- Modify: `docs/config.reference.toml` — add `[mcp]` section
- Modify: `docs/api.md` — add MCP endpoint documentation

- [ ] **Step 1: Update CLAUDE.md env var table**

Add the `CETACEAN_MCP*` variables to the environment variables table in `CLAUDE.md`.

- [ ] **Step 2: Update config reference**

Add `[mcp]` section to `docs/config.reference.toml` with all MCP options and their defaults.

- [ ] **Step 3: Update API docs**

Add MCP endpoint documentation to `docs/api.md`: the `/mcp` endpoint, OAuth endpoints, supported JSON-RPC methods, resource URIs, and tool names.

- [ ] **Step 4: Update architecture section in CLAUDE.md**

Add `internal/cluster/` and `internal/mcp/` package descriptions to the Backend architecture section.

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md docs/config.reference.toml docs/api.md
git commit -m "docs: add MCP server documentation"
```
