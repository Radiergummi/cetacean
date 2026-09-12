# One Public URL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace three unrelated answers to "what is my external URL?" with one `server.public_url` setting, and stop the MCP issuer defaulting to the hostless `http://:9000`.

**Architecture:** Task 1 fixes the hostless issuer on its own, moving the derivation out of the untested `main.go` into `internal/config` where it can be tested. Task 2 adds `server.public_url` with no consumers. Tasks 3–5 point each of the three consumers at it, in isolation. Task 6 documents the result.

**Tech Stack:** Go 1.26, stdlib `net/url` and `net/http`, `log/slog`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-07-public-url-design.md`

## Global Constraints

- `server.public_url` is **origin only**: scheme + host + optional port. A path, query or fragment is a config error.
- User-facing messages name settings by their **TOML path** (`server.public_url`, not `CETACEAN_PUBLIC_URL`).
- Precedence everywhere: explicit setting > `public_url` > existing fallback.
- `server.cors.origins` is out of scope — it lists third-party origins, not Cetacean's own.
- Every task ends green on `make check` (`golangci-lint` + `oxlint` + gofmt/oxfmt + `go test ./...`).
- Config precedence in this codebase is flag > env > `env_FILE` > config file > default, implemented by `resolve` in `internal/config/resolve.go`. Do not hand-roll it.

---

### Task 1: Stop deriving a hostless MCP issuer

**Files:**
- Modify: `internal/config/mcp.go` (append at end of file)
- Modify: `main.go:689-696` (the `issuer :=` block inside `setupMCP`)
- Test: `internal/config/mcp_test.go` (append)

**Interfaces:**
- Consumes: `Config.MCP.Issuer`, `Config.ListenAddr` (both already exist).
- Produces: `func (c *Config) MCPIssuer(tlsEnabled bool) (string, bool)` — the issuer, and whether it is actually reachable. Task 3 modifies this function; Task 6 documents its behaviour.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/mcp_test.go`:

```go
func TestMCPIssuer(t *testing.T) {
	tests := []struct {
		name       string
		issuer     string
		listenAddr string
		tlsEnabled bool
		want       string
		wantOK     bool
	}{
		{
			name:       "explicit issuer wins",
			issuer:     "https://cetacean.example.com",
			listenAddr: ":9000",
			want:       "https://cetacean.example.com",
			wantOK:     true,
		},
		{
			name:       "default listen address has no host",
			listenAddr: ":9000",
			want:       "http://:9000",
			wantOK:     false,
		},
		{
			name:       "wildcard bind is not reachable",
			listenAddr: "0.0.0.0:9000",
			want:       "http://0.0.0.0:9000",
			wantOK:     false,
		},
		{
			name:       "unspecified IPv6 bind is not reachable",
			listenAddr: "[::]:9000",
			want:       "http://[::]:9000",
			wantOK:     false,
		},
		{
			name:       "explicit host derives",
			listenAddr: "cetacean.internal:9000",
			want:       "http://cetacean.internal:9000",
			wantOK:     true,
		},
		{
			name:       "TLS derives https",
			listenAddr: "cetacean.internal:9000",
			tlsEnabled: true,
			want:       "https://cetacean.internal:9000",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{ListenAddr: tt.listenAddr, MCP: MCPConfig{Issuer: tt.issuer}}

			got, ok := cfg.MCPIssuer(tt.tlsEnabled)

			if got != tt.want {
				t.Errorf("issuer = %q, want %q", got, tt.want)
			}
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestMCPIssuer -v`
Expected: FAIL — `cfg.MCPIssuer undefined (type *Config has no field or method MCPIssuer)`

- [ ] **Step 3: Write the implementation**

Append to `internal/config/mcp.go`:

```go
// MCPIssuer returns the canonical external base URL clients reach this
// deployment at: mcp.issuer when set, otherwise derived from
// server.listen_addr and whether TLS terminates here.
//
// The second return is false when the derivation produced a URL nothing can
// reach. server.listen_addr defaults to ":9000", so the derived issuer is
// "http://:9000" — a URL with an empty host, which resolveMCPIssuer rejects
// when an operator types it. A wildcard bind ("0.0.0.0", "::") parses to a
// host but is equally unreachable.
//
// The string is returned either way, because how bad an unreachable issuer is
// depends on the caller: OAuth clients fetch .well-known documents from it and
// cannot work at all, while auth mode "none" builds no OAuth server and ends
// up only with unreachable icon URLs.
func (c *Config) MCPIssuer(tlsEnabled bool) (string, bool) {
	if c.MCP.Issuer != "" {
		return c.MCP.Issuer, true
	}

	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	issuer := scheme + "://" + c.ListenAddr

	u, err := url.Parse(issuer)
	if err != nil {
		return issuer, false
	}

	switch u.Hostname() {
	case "", "0.0.0.0", "::":
		return issuer, false
	}

	return issuer, true
}
```

`net/url` is already imported by `internal/config/mcp.go` (used by `resolveMCPIssuer`); do not add it again.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestMCPIssuer -v`
Expected: PASS, all six subtests.

- [ ] **Step 5: Use it from setupMCP**

In `main.go`, replace the block at lines 689-696:

```go
	issuer := d.cfg.MCP.Issuer
	if issuer == "" {
		scheme := "http"
		if d.tlsEnabled {
			scheme = "https"
		}
		issuer = scheme + "://" + d.cfg.ListenAddr
	}
	mcpResource := issuer + d.cfg.BasePath + "/mcp"
```

with:

```go
	issuer, reachable := d.cfg.MCPIssuer(d.tlsEnabled)
	if !reachable {
		if d.authMode != "none" {
			slog.Error(
				"MCP OAuth needs an issuer clients can reach, and none could be derived from server.listen_addr. Set mcp.issuer to the URL clients reach from outside.",
				"derived_issuer", issuer,
				"listen_addr", d.cfg.ListenAddr,
			)
			os.Exit(1)
		}
		slog.Warn(
			"no reachable MCP issuer could be derived from server.listen_addr; MCP tool icons will point at an unreachable URL. Set mcp.issuer to the URL clients reach from outside.",
			"derived_issuer", issuer,
			"listen_addr", d.cfg.ListenAddr,
		)
	}
	mcpResource := issuer + d.cfg.BasePath + "/mcp"
```

Also update the doc comment above `setupMCP` (`main.go:671-673`), replacing:

```go
// The issuer defaults to the listen address + TLS scheme, which only works
// when no reverse proxy is in front. Behind a proxy, set CETACEAN_MCP_ISSUER
// (or [mcp].issuer) to the canonical external URL.
```

with:

```go
// The issuer defaults to the listen address + TLS scheme, which only works
// when the listen address carries a real host and no reverse proxy is in
// front. Startup fails when OAuth is in play and no reachable issuer could be
// derived; see Config.MCPIssuer.
```

- [ ] **Step 6: Verify the whole build and suite**

Run: `go build ./... && go test ./...`
Expected: PASS. `main.go` has no test file, so the build is the gate here.

- [ ] **Step 7: Commit**

```bash
git add internal/config/mcp.go internal/config/mcp_test.go main.go
git commit -m "fix(mcp): stop deriving an issuer nothing can reach

server.listen_addr defaults to \":9000\", so the derived issuer was
\"http://:9000\" — a URL with no host, which resolveMCPIssuer rejects when an
operator types it but the fallback never reached its validator. It feeds the
OAuth issuer, the resource audience and the tool icon base, so it was wrong in
every auth mode.

Move the derivation into Config.MCPIssuer, where it can be tested, and report
whether the result is reachable. Startup now fails when OAuth needs an issuer
and none could be derived; auth mode \"none\" builds no OAuth server, so it
warns instead of breaking deployments that work today."
```

---

### Task 2: Add the server.public_url setting

**Files:**
- Create: `internal/config/publicurl.go`
- Create: `internal/config/publicurl_test.go`
- Modify: `internal/config/config.go` (the `Config` struct near line 38, the file-pointer block near line 84, the `cfg := &Config{...}` literal near line 145, and the validation after it near line 182)
- Modify: `internal/config/file.go` (`fileServer` struct)
- Modify: `internal/config/flags.go` (near line 188 beside `corsOrigins`, and near line 273 in the visited-flag block)

**Interfaces:**
- Produces: `Config.PublicURL string` (empty when unset, trailing slash stripped) and `func ValidatePublicURL(raw string) error`. Tasks 3, 4 and 5 all read `Config.PublicURL`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/publicurl_test.go`:

```go
package config

import "testing"

func TestValidatePublicURL(t *testing.T) {
	valid := []string{
		"",
		"https://cetacean.example.com",
		"http://localhost:9000",
		"https://cetacean.example.com:8443",
		"http://10.0.0.5:9000",
	}

	for _, raw := range valid {
		if err := ValidatePublicURL(raw); err != nil {
			t.Errorf("ValidatePublicURL(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []struct {
		raw     string
		wantSub string
	}{
		{"ftp://cetacean.example.com", "http or https"},
		{"cetacean.example.com", "http or https"},
		{"https://", "must include a host"},
		{"http://:9000", "must include a host"},
		{"https://cetacean.example.com/cetacean", "server.base_path"},
		{"https://cetacean.example.com?a=b", "query or fragment"},
		{"https://cetacean.example.com#frag", "query or fragment"},
	}

	for _, tt := range invalid {
		err := ValidatePublicURL(tt.raw)
		if err == nil {
			t.Errorf("ValidatePublicURL(%q) = nil, want error", tt.raw)
			continue
		}
		if !strings.Contains(err.Error(), tt.wantSub) {
			t.Errorf("ValidatePublicURL(%q) = %q, want it to mention %q", tt.raw, err, tt.wantSub)
		}
	}
}

func TestLoadPublicURLStripsTrailingSlash(t *testing.T) {
	t.Setenv("CETACEAN_PUBLIC_URL", "https://cetacean.example.com/")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.PublicURL != "https://cetacean.example.com" {
		t.Errorf("PublicURL = %q, want %q", cfg.PublicURL, "https://cetacean.example.com")
	}
}

func TestLoadRejectsPublicURLWithPath(t *testing.T) {
	t.Setenv("CETACEAN_PUBLIC_URL", "https://cetacean.example.com/cetacean")

	if _, err := Load(nil, nil); err == nil {
		t.Fatal("Load = nil error, want a rejection naming server.base_path")
	}
}
```

Add `"strings"` to that file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestValidatePublicURL|TestLoadPublicURL|TestLoadRejectsPublicURL' -v`
Expected: FAIL — `undefined: ValidatePublicURL` and `cfg.PublicURL undefined`.

- [ ] **Step 3: Write the validator**

Create `internal/config/publicurl.go`:

```go
package config

import (
	"fmt"
	"net/url"
)

// ValidatePublicURL checks server.public_url is an http(s) origin: a scheme
// and a host, and nothing after them.
//
// A path is rejected rather than accepted and ignored, because
// server.base_path already carries the external prefix in both directions —
// basePathMiddleware strips it from inbound requests and absPath prepends it
// to outbound links — and two settings for one concept drift apart.
//
// An empty value is valid and means "unset": every consumer keeps the
// fallback it had before this setting existed.
func ValidatePublicURL(raw string) error {
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("server.public_url is not a valid URL %q: %w", raw, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf(
			"server.public_url must use http or https, got %q",
			raw,
		)
	}

	if u.Hostname() == "" {
		return fmt.Errorf("server.public_url must include a host, got %q", raw)
	}

	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf(
			"server.public_url must not include a path, got %q; set server.base_path to %q instead",
			raw, u.Path,
		)
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("server.public_url must not include a query or fragment, got %q", raw)
	}

	return nil
}
```

- [ ] **Step 4: Wire it into Config**

In `internal/config/config.go`, add to the `Config` struct immediately after `BasePath`:

```go
	// PublicURL is the canonical external origin clients reach this
	// deployment at, e.g. "https://cetacean.example.com". Origin only: the
	// external path prefix is BasePath, which absPath already prepends to
	// outbound links. Empty means every consumer keeps its own fallback.
	PublicURL string // CETACEAN_PUBLIC_URL, default ""
```

In the file-pointer block (beside `fBasePath`), declare `fPublicURL *string` and populate it in the same `if fc.Server != nil` branch that sets `fBasePath`:

```go
			fPublicURL = fc.Server.PublicURL
```

In the `cfg := &Config{...}` literal, immediately after the `BasePath:` entry:

```go
		PublicURL: strings.TrimRight(
			resolve(flags.PublicURL, "CETACEAN_PUBLIC_URL", fPublicURL, ""),
			"/",
		),
```

And immediately after the existing `ValidateBasePath` check:

```go
	if err := ValidatePublicURL(cfg.PublicURL); err != nil {
		return nil, err
	}
```

`strings` is already imported by `internal/config/config.go` (line 7); no import change needed.

In `internal/config/file.go`, add to `fileServer` after `BasePath`:

```go
	PublicURL       *string   `toml:"public_url"`
```

In `internal/config/flags.go`, add to the `Flags` struct beside `CORSOrigins`:

```go
	PublicURL        *string
```

register the flag beside `corsOrigins` (near line 188):

```go
	publicURL := fs.String(
		"public-url",
		"",
		"Canonical external URL, e.g. https://cetacean.example.com (env: CETACEAN_PUBLIC_URL)",
	)
```

and set it in the visited-flag block beside `f.CORSOrigins` (near line 273):

```go
		case "public-url":
			f.PublicURL = publicURL
```

Match the exact shape of the surrounding cases in that block — read lines 260-285 first and copy the idiom rather than guessing it.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS, including the three new tests and every pre-existing config test.

- [ ] **Step 6: Commit**

```bash
git add internal/config/publicurl.go internal/config/publicurl_test.go \
        internal/config/config.go internal/config/file.go internal/config/flags.go
git commit -m "feat(config): add server.public_url

The canonical external origin, as one setting. Origin only: server.base_path
already carries the external path prefix in both directions, so a path here is
rejected with an error naming it rather than silently accepted.

No consumers yet."
```

---

### Task 3: Default the MCP issuer from public_url

**Files:**
- Modify: `internal/config/mcp.go` (`MCPIssuer`, added in Task 1)
- Modify: `main.go` (the `slog.Error`/`slog.Warn` messages added in Task 1)
- Test: `internal/config/mcp_test.go` (extend `TestMCPIssuer`)

**Interfaces:**
- Consumes: `Config.PublicURL` (Task 2), `func (c *Config) MCPIssuer(tlsEnabled bool) (string, bool)` (Task 1).
- Produces: no new symbols; `MCPIssuer`'s precedence becomes `mcp.issuer` > `server.public_url` > derived.

- [ ] **Step 1: Write the failing test**

Add these two cases to the `tests` slice in `TestMCPIssuer`, and add a `publicURL string` field to the struct alongside `issuer`:

```go
		{
			name:       "public_url is used when mcp.issuer is unset",
			publicURL:  "https://cetacean.example.com",
			listenAddr: ":9000",
			want:       "https://cetacean.example.com",
			wantOK:     true,
		},
		{
			name:       "mcp.issuer overrides public_url",
			issuer:     "https://mcp.example.com",
			publicURL:  "https://cetacean.example.com",
			listenAddr: ":9000",
			want:       "https://mcp.example.com",
			wantOK:     true,
		},
```

and change the `cfg` construction inside the subtest to:

```go
			cfg := &Config{
				ListenAddr: tt.listenAddr,
				PublicURL:  tt.publicURL,
				MCP:        MCPConfig{Issuer: tt.issuer},
			}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestMCPIssuer -v`
Expected: FAIL on `public_url is used when mcp.issuer is unset` — `issuer = "http://:9000", want "https://cetacean.example.com"`.

- [ ] **Step 3: Write the implementation**

In `internal/config/mcp.go`, insert into `MCPIssuer` immediately after the `c.MCP.Issuer` check:

```go
	if c.PublicURL != "" {
		return c.PublicURL, true
	}
```

and extend the doc comment's first paragraph to read:

```go
// MCPIssuer returns the canonical external base URL clients reach this
// deployment at: mcp.issuer when set, then server.public_url, otherwise
// derived from server.listen_addr and whether TLS terminates here.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestMCPIssuer -v`
Expected: PASS, all eight subtests.

- [ ] **Step 5: Point the startup messages at public_url**

In `main.go`, change the two message strings added in Task 1 so they name the shared setting first:

```go
			slog.Error(
				"MCP OAuth needs an issuer clients can reach, and none could be derived from server.listen_addr. Set server.public_url to the URL clients reach from outside, or mcp.issuer to override it for MCP alone.",
				"derived_issuer", issuer,
				"listen_addr", d.cfg.ListenAddr,
			)
```

```go
		slog.Warn(
			"no reachable MCP issuer could be derived from server.listen_addr; MCP tool icons will point at an unreachable URL. Set server.public_url to the URL clients reach from outside.",
			"derived_issuer", issuer,
			"listen_addr", d.cfg.ListenAddr,
		)
```

- [ ] **Step 6: Verify the build and suite**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/mcp.go internal/config/mcp_test.go main.go
git commit -m "feat(mcp): default the issuer from server.public_url

mcp.issuer stays as the per-server override; it no longer has to be set
separately just to name the deployment's own URL."
```

---

### Task 4: Default the OIDC redirect URL from public_url

**Files:**
- Modify: `internal/config/auth.go` (the `LoadAuth` signature at line 57, the `RedirectURL:` entry near line 117, and a new unexported helper at the end of the file)
- Modify: `main.go:109` (the `config.LoadAuth` call)
- Test: `internal/config/auth_test.go` (append; and update every existing `LoadAuth(` call in the package)

**Interfaces:**
- Consumes: `Config.PublicURL`, `Config.BasePath`.
- Produces: `func LoadAuth(flags *Flags, fc *fileConfig, publicURL, basePath string) (*AuthConfig, error)` — a signature change every caller and test must follow.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/auth_test.go`:

```go
func TestOIDCRedirectURLDefaultsFromPublicURL(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "cetacean")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")

	cfg, err := LoadAuth(nil, nil, "https://cetacean.example.com", "")
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}

	want := "https://cetacean.example.com/auth/callback"
	if cfg.OIDC.RedirectURL != want {
		t.Errorf("RedirectURL = %q, want %q", cfg.OIDC.RedirectURL, want)
	}
}

func TestOIDCRedirectURLIncludesBasePath(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "cetacean")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")

	cfg, err := LoadAuth(nil, nil, "https://cetacean.example.com", "/cetacean")
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}

	want := "https://cetacean.example.com/cetacean/auth/callback"
	if cfg.OIDC.RedirectURL != want {
		t.Errorf("RedirectURL = %q, want %q", cfg.OIDC.RedirectURL, want)
	}
}

func TestOIDCRedirectURLExplicitWins(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "cetacean")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("CETACEAN_AUTH_OIDC_REDIRECT_URL", "https://other.example.com/auth/callback")

	cfg, err := LoadAuth(nil, nil, "https://cetacean.example.com", "")
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}

	want := "https://other.example.com/auth/callback"
	if cfg.OIDC.RedirectURL != want {
		t.Errorf("RedirectURL = %q, want %q", cfg.OIDC.RedirectURL, want)
	}
}

func TestOIDCStillRequiresRedirectURLWithoutPublicURL(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "oidc")
	t.Setenv("CETACEAN_AUTH_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_ID", "cetacean")
	t.Setenv("CETACEAN_AUTH_OIDC_CLIENT_SECRET", "secret")

	if _, err := LoadAuth(nil, nil, "", ""); err == nil {
		t.Fatal("LoadAuth = nil error, want the existing 'oidc mode requires' rejection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestOIDCRedirectURL -v`
Expected: FAIL to compile — `too many arguments in call to LoadAuth`.

- [ ] **Step 3: Write the implementation**

Change the signature at `internal/config/auth.go:57`:

```go
func LoadAuth(flags *Flags, fc *fileConfig, publicURL, basePath string) (*AuthConfig, error) {
```

and extend its doc comment with:

```go
// publicURL and basePath come from Config and supply the default OIDC
// redirect URL; pass "" for both to keep auth.oidc.redirect_url required.
```

Change the `RedirectURL:` entry (near line 117) so its default is derived rather than empty:

```go
			RedirectURL: resolve(
				flags.OIDCRedirectURL,
				"CETACEAN_AUTH_OIDC_REDIRECT_URL",
				fileField(fo, func(o *fileAuthOIDC) *string { return o.RedirectURL }),
				defaultRedirectURL(publicURL, basePath),
			),
```

Add at the end of `internal/config/auth.go`:

```go
// defaultRedirectURL builds the OIDC callback URL from server.public_url. The
// callback route is fixed at GET /auth/callback (internal/auth/oidc.go), so
// the value is mechanically derivable and only has to be typed when the
// callback lives somewhere else.
//
// Returns "" when public_url is unset, which leaves the existing "oidc mode
// requires ..." rejection in place rather than inventing a URL.
func defaultRedirectURL(publicURL, basePath string) string {
	if publicURL == "" {
		return ""
	}

	return publicURL + basePath + "/auth/callback"
}
```

A derived `http://` URL still fails `validateRedirectURL` (HTTPS required per OAuth 2.1, loopback exempt) with its existing message. That is correct: an OIDC deployment on plain HTTP was never valid.

- [ ] **Step 4: Update every caller**

Run: `grep -rn 'LoadAuth(' --include='*.go' .`

Update `main.go:109` to:

```go
	authCfg, err := config.LoadAuth(flags, fc, cfg.PublicURL, cfg.BasePath)
```

`cfg` is already in scope there — it is assigned at `main.go:89`, before this call. Update every `LoadAuth(` call in `internal/config/auth_test.go` to pass `, "", ""` so existing expectations are unchanged.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v && go build ./...`
Expected: PASS, including the four new tests and every pre-existing auth test.

- [ ] **Step 6: Commit**

```bash
git add internal/config/auth.go internal/config/auth_test.go main.go
git commit -m "feat(auth): default the OIDC redirect URL from server.public_url

The callback route is fixed at GET /auth/callback, so the value was always
mechanically derivable — and internal/auth/oidc.go already reverse-engineered
the origin back out of it to build the post-logout redirect.

auth.oidc.redirect_url stays as the override for a callback that lives
somewhere else, and stays required when public_url is unset."
```

---

### Task 5: Build feed links from public_url instead of forwarded headers

**Files:**
- Modify: `internal/api/basepath.go` (add a context key, accessor and middleware; change `absURL` at line 36)
- Modify: `internal/api/feed_handlers.go:390` (the Atom `tag:` URI)
- Modify: `internal/api/router.go` (the `RouterConfig` struct, and line 679)
- Modify: `main.go:453-463` (the `api.RouterConfig{...}` literal)
- Test: `internal/api/basepath_test.go` (append)

**Interfaces:**
- Consumes: `Config.PublicURL`.
- Produces: `func PublicURLFromContext(ctx context.Context) string`, `func publicURLMiddleware(publicURL string, next http.Handler) http.Handler`, and `RouterConfig.PublicURL string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/basepath_test.go`:

```go
func TestAbsURLPrefersPublicURL(t *testing.T) {
	handler := publicURLMiddleware(
		"https://cetacean.example.com",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := absURL(r, "/services"); got != "https://cetacean.example.com/services" {
				t.Errorf("absURL = %q, want %q", got, "https://cetacean.example.com/services")
			}
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.Host = "internal:9000"
	req.Header.Set("X-Forwarded-Host", "attacker.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestAbsURLFallsBackToForwardedHeaders(t *testing.T) {
	handler := publicURLMiddleware(
		"",
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := absURL(r, "/services"); got != "https://proxy.example.com/services" {
				t.Errorf("absURL = %q, want %q", got, "https://proxy.example.com/services")
			}
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	req.Host = "internal:9000"
	req.Header.Set("X-Forwarded-Host", "proxy.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	handler.ServeHTTP(httptest.NewRecorder(), req)
}
```

`internal/api/basepath_test.go` already imports `net/http`, `net/http/httptest` and `testing`; no import change needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestAbsURL -v`
Expected: FAIL — `undefined: publicURLMiddleware`.

- [ ] **Step 3: Write the implementation**

In `internal/api/basepath.go`, beside the existing `basePathCtxKey` declaration:

```go
type publicURLCtxKey struct{}

var publicURLKey = publicURLCtxKey{}

// PublicURLFromContext extracts the configured external origin.
// Returns "" when server.public_url is unset.
func PublicURLFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(publicURLKey).(string); ok {
		return v
	}
	return ""
}

// publicURLMiddleware stores server.public_url in the request context so
// absURL can build links from configuration rather than from X-Forwarded-*,
// which nothing validates against server.trusted_proxies — any client can set
// those headers. A no-op when public_url is unset.
func publicURLMiddleware(publicURL string, next http.Handler) http.Handler {
	if publicURL == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), publicURLKey, publicURL)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

Change `absURL` so it prefers the configured origin, keeping the rest untouched:

```go
func absURL(r *http.Request, path string) string {
	if base := PublicURLFromContext(r.Context()); base != "" {
		return base + absPath(r.Context(), path)
	}

	scheme := "http"
	...
```

and extend its doc comment:

```go
// absURL builds a full absolute URL (scheme://host/base/path) from the
// request. Uses server.public_url when configured; otherwise
// X-Forwarded-Proto/Host, falling back to r.TLS and r.Host. Intended for Atom
// feeds where RFC 4287 requires IRIs.
```

In `internal/api/feed_handlers.go`, replace line 390:

```go
	return fmt.Sprintf("tag:%s,2026:%s", r.Host, absPath(r.Context(), r.URL.Path))
```

with:

```go
	// RFC 4151 tag URIs are permanent identifiers, so prefer the configured
	// origin: derived from r.Host, an entry's identity changes with the
	// hostname a reader happened to reach the server by.
	host := r.Host
	if base := PublicURLFromContext(r.Context()); base != "" {
		if u, err := url.Parse(base); err == nil {
			host = u.Host
		}
	}

	return fmt.Sprintf("tag:%s,2026:%s", host, absPath(r.Context(), r.URL.Path))
```

Add `"net/url"` to `internal/api/feed_handlers.go` imports if absent.

- [ ] **Step 4: Wire the middleware**

Add to the `RouterConfig` struct in `internal/api/router.go`, beside `BasePath`:

```go
	PublicURL string
```

and change line 679 from:

```go
	return basePathMiddleware(cfg.BasePath, handler)
```

to:

```go
	return publicURLMiddleware(cfg.PublicURL, basePathMiddleware(cfg.BasePath, handler))
```

In `main.go`, add `PublicURL:         cfg.PublicURL,` to the `api.RouterConfig{...}` literal (line 453) immediately after its `BasePath: cfg.BasePath,` entry at line 463, matching the field alignment of the surrounding literal.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -v && go build ./...`
Expected: PASS, including the two new tests and every pre-existing Atom/JSON Feed test.

- [ ] **Step 6: Commit**

```bash
git add internal/api/basepath.go internal/api/basepath_test.go \
        internal/api/feed_handlers.go internal/api/router.go main.go
git commit -m "feat(api): build feed links from server.public_url

absURL read X-Forwarded-Proto/Host, which no middleware validates against
server.trusted_proxies, so any client could dictate the links in a feed. And
the Atom tag: URI came from r.Host, making an RFC 4151 permanent identifier
vary with the hostname a reader used.

Both now prefer the configured origin, falling back to the old behaviour when
server.public_url is unset."
```

---

### Task 6: Document server.public_url

**Files:**
- Modify: `docs/configuration.mdx` (add a `ConfigParam` for `server.public_url` in the server section; amend `mcp.issuer` and `auth.oidc.redirect_url`)
- Modify: `docs/mcp.md` (the `[!WARNING]` callout under "Turn it on")
- Modify: `docs/authentication.md` (the OIDC section's redirect URL guidance)
- Modify: `CLAUDE.md` (the environment variable table)
- Modify: `CHANGELOG.md` (under `[Unreleased]`)

**Interfaces:**
- Consumes: everything from Tasks 1–5. Produces no code.

- [ ] **Step 1: Add the setting to the configuration reference**

In `docs/configuration.mdx`, place this immediately after the `server.base_path` entry:

```mdx
<ConfigParam name="server.public_url" flag="-public-url" env="CETACEAN_PUBLIC_URL">
  Canonical external URL clients reach Cetacean at, e.g. `https://cetacean.example.com`. Origin only — set the path prefix with [`server.base_path`][server.base_path], which is used for both inbound and outbound paths. Supplies the default for [`mcp.issuer`][mcp.issuer] and [`auth.oidc.redirect_url`][auth.oidc.redirect_url], and makes Atom and JSON Feed links independent of the `X-Forwarded-*` headers a client can set. Set it behind a reverse proxy.
</ConfigParam>
```

Add the matching link definitions to the bottom of the file if `[server.base_path]`, `[mcp.issuer]` or `[auth.oidc.redirect_url]` are not already defined there; follow the existing `configuration#<name>` pattern.

- [ ] **Step 2: Amend the two settings it now defaults**

Change the `mcp.issuer` description's last sentence from:

```
Derived from the listen address and TLS settings when unset, so set it behind a reverse proxy.
```

to:

```
Defaults to [`server.public_url`][server.public_url]; set this only to give MCP a different URL from the rest of Cetacean. With neither set, it is derived from the listen address, and startup fails if that yields no reachable host.
```

Remove the `required` prop from the `auth.oidc.redirect_url` `ConfigParam` and end its description with:

```
Defaults to `{server.public_url}{server.base_path}/auth/callback`; required only when [`server.public_url`][server.public_url] is unset, or when the callback lives elsewhere.
```

- [ ] **Step 3: Update the MCP guide**

In `docs/mcp.md`, replace the `[!WARNING]` callout under "Turn it on" with:

```markdown
> [!WARNING]
> [`server.public_url`][server.public_url] is the URL clients reach from outside the cluster, not a service name on
> the overlay network. Getting it wrong breaks sign-in, and leaving it unset behind a proxy stops startup.
```

Change the `CETACEAN_MCP_ISSUER` line in the YAML example above it to `CETACEAN_PUBLIC_URL`, and add a `[server.public_url]: configuration#server.public_url` definition to the link block at the bottom of the file.

Update the Troubleshooting row that reads "`mcp.issuer` is not the URL clients reach from outside." to name `server.public_url` instead.

- [ ] **Step 4: Update the authentication guide**

In `docs/authentication.md`, in the OIDC section, add after the settings list:

```markdown
> [!NOTE]
> With [`server.public_url`][server.public_url] set, `auth.oidc.redirect_url` is derived as
> `{public_url}{base_path}/auth/callback` and only needs setting if your callback lives elsewhere.
```

Add the link definition to that file's link block.

- [ ] **Step 5: Update CLAUDE.md and the changelog**

Add to the environment variable table in `CLAUDE.md`, after `CETACEAN_BASE_PATH`:

```
| `CETACEAN_PUBLIC_URL` | — | No (canonical external URL; defaults `mcp.issuer` and `auth.oidc.redirect_url`) |
```

Add under `## [Unreleased]` in `CHANGELOG.md`, in the existing `### Added` / `### Fixed` subsections (creating them if absent):

```markdown
### Added

- `server.public_url` sets the canonical external URL once, supplying the OAuth issuer for the MCP server and the OIDC redirect URL instead of configuring each separately.

### Fixed

- The MCP server no longer advertises an unreachable OAuth issuer when the listen address has no host. Startup now stops and says what to set.
- Atom and JSON Feed links are built from `server.public_url` when it is set, rather than from request headers a client can control.
```

- [ ] **Step 6: Verify the docs build and render**

Run: `cd website && npm run build`
Expected: build completes; `dist/configuration.html` contains `server.public_url`.

Verify the callouts still render: `grep -c 'class="callout' website/dist/mcp.html website/dist/authentication.html`
Expected: at least 1 and 3 respectively.

- [ ] **Step 7: Commit**

```bash
git add docs/configuration.mdx docs/mcp.md docs/authentication.md CLAUDE.md CHANGELOG.md
git commit -m "docs: document server.public_url"
```

---

## Self-review notes

- **Spec coverage.** Problem 1 → Tasks 1 and 3; problem 2 → Task 4; problem 3 → Task 5. The origin-only decision → Task 2's validator. The fail-vs-warn rule → Task 1 Step 5. The CORS non-goal is not implemented anywhere, as intended.
- **Type consistency.** `MCPIssuer(tlsEnabled bool) (string, bool)` is defined in Task 1 and extended in Task 3 with no signature change. `LoadAuth`'s new signature appears in Task 4 only, with a step that updates every caller. `PublicURLFromContext` and `publicURLMiddleware` appear only in Task 5.
- **Known ordering constraint.** Task 3 rewrites two log messages introduced in Task 1. That churn is deliberate: the bug fix ships independently of the new setting, as requested.
