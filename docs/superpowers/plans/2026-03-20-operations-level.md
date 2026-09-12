# Operations Level Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tiered `CETACEAN_OPERATIONS_LEVEL` setting (0–2) that gates write endpoints by danger class, allowing operators to restrict Cetacean to read-only or limit it to safe operational actions.

**Architecture:** A new `OperationsLevel` int field in `Config` (env `CETACEAN_OPERATIONS_LEVEL`, TOML `server.operations_level`, default `1`). The existing `requireWrite` middleware becomes `requireLevel(level int, cfg *Config)` and returns 403 with an RFC 9457 problem detail when the configured level is insufficient. Each write route registration passes its required tier. The health endpoint exposes the configured level so the frontend can hide disabled actions.

**Tech Stack:** Go stdlib, existing config resolution pattern, existing test patterns

**Tier classification:**
| Tier | Name | Operations |
|------|------|------------|
| 0 | Read-only | No write operations |
| 1 | Operational | scale, image update, rollback, restart, patch env, patch service labels, patch resources, put/patch healthcheck |
| 2 | Impactful | node availability, node labels, service mode, service endpoint mode, remove task |

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `internal/config/resolve.go` | Add `resolveInt` helper |
| Modify | `internal/config/config.go` | Add `OperationsLevel` field + resolution |
| Modify | `internal/config/file.go` | Add `OperationsLevel` to `fileServer` |
| Modify | `internal/api/write_middleware.go` | Replace `requireWrite` with `requireLevel` |
| Modify | `internal/api/router.go` | Pass tier per write route, thread `Config` through |
| Modify | `internal/api/handlers.go` | Add `operationsLevel` to `Handlers`, expose in health |
| Modify | `internal/config/config_test.go` | Test `OperationsLevel` resolution |
| Modify | `internal/api/write_middleware_test.go` | Test `requireLevel` at all tier boundaries |
| Modify | `main.go` | Pass `OperationsLevel` through to handlers |

---

### Task 1: Add `resolveInt` to config resolution

**Files:**
- Modify: `internal/config/resolve.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestLoad_OperationsLevel_Default(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OperationsLevel != 1 {
		t.Errorf("OperationsLevel=%d, want 1", cfg.OperationsLevel)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoad_OperationsLevel_Default -v`
Expected: FAIL — `cfg.OperationsLevel` does not exist

- [ ] **Step 3: Add `resolveInt` to `resolve.go`**

Append to `internal/config/resolve.go`:

```go
// resolveInt returns the first set value in precedence order:
// flag > env > file > hardcoded default. Returns an error if any
// explicitly set value is not a valid integer or is out of [min, max].
func resolveInt(flag *int, envKey string, file *int, def, min, max int) (int, error) {
	var raw string
	var source string
	switch envVal := os.Getenv(envKey); {
	case flag != nil:
		return clampInt(*flag, min, max, "flag")
	case envVal != "":
		raw, source = envVal, envKey
	case file != nil:
		return clampInt(*file, min, max, "config file")
	default:
		return def, nil
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid integer from %s %q: %w", source, raw, err)
	}
	return clampInt(v, min, max, source)
}

func clampInt(v, min, max int, source string) (int, error) {
	if v < min || v > max {
		return 0, fmt.Errorf("value %d from %s out of range [%d, %d]", v, source, min, max)
	}
	return v, nil
}
```

Note: add `"strconv"` to the imports.

- [ ] **Step 4: Add `OperationsLevel` to Config struct and `Load`**

In `internal/config/config.go`, add to the `Config` struct:

```go
OperationsLevel int // CETACEAN_OPERATIONS_LEVEL, 0=read-only, 1=operational, 2=impactful
```

In `internal/config/file.go`, add to `fileServer`:

```go
OperationsLevel *int `toml:"operations_level"`
```

In `Load()`, extract the file pointer alongside others in the `fc.Server` block:

```go
fOpsLevel *int
```

Inside `if fc.Server != nil`:

```go
fOpsLevel = fc.Server.OperationsLevel
```

Then resolve it (before constructing `cfg`):

```go
opsLevel, err := resolveInt(nil, "CETACEAN_OPERATIONS_LEVEL", fOpsLevel, 1, 0, 2)
if err != nil {
	return nil, err
}
```

And set in the `cfg` struct literal:

```go
OperationsLevel: opsLevel,
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestLoad_OperationsLevel -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/resolve.go internal/config/config.go internal/config/file.go internal/config/config_test.go
git commit -m "feat(config): add OperationsLevel setting (CETACEAN_OPERATIONS_LEVEL)"
```

---

### Task 2: Test `resolveInt` edge cases

**Files:**
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write env-override and range-validation tests**

Add to `internal/config/config_test.go`:

```go
func TestLoad_OperationsLevel_EnvOverride(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "0")

	cfg, err := Load(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OperationsLevel != 0 {
		t.Errorf("OperationsLevel=%d, want 0", cfg.OperationsLevel)
	}
}

func TestLoad_OperationsLevel_FileOverride(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "")

	level := 2
	fc := &fileConfig{
		Server: &fileServer{OperationsLevel: &level},
	}

	cfg, err := Load(fc, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OperationsLevel != 2 {
		t.Errorf("OperationsLevel=%d, want 2", cfg.OperationsLevel)
	}
}

func TestLoad_OperationsLevel_OutOfRange(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "5")

	_, err := Load(nil, nil)
	if err == nil {
		t.Fatal("expected error for out-of-range value")
	}
}

func TestLoad_OperationsLevel_Invalid(t *testing.T) {
	t.Setenv("CETACEAN_OPERATIONS_LEVEL", "banana")

	_, err := Load(nil, nil)
	if err == nil {
		t.Fatal("expected error for non-integer value")
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestLoad_OperationsLevel -v`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test(config): add OperationsLevel edge case tests"
```

---

### Task 3: Replace `requireWrite` with `requireLevel`

**Files:**
- Modify: `internal/api/write_middleware.go`

- [ ] **Step 1: Write the failing test**

Replace the contents of `internal/api/write_middleware_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	json "github.com/goccy/go-json"
)

func TestRequireLevel_Allowed(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireLevel(1, 2)(inner)
	req := httptest.NewRequest("PUT", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestRequireLevel_Denied(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := requireLevel(2, 1)(inner)
	req := httptest.NewRequest("PUT", "/nodes/abc/availability", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Error("inner handler should not be called when level is insufficient")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403", w.Code)
	}

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("failed to decode problem: %v", err)
	}
	if p.Status != 403 {
		t.Errorf("problem status=%d, want 403", p.Status)
	}
}

func TestRequireLevel_ReadOnly(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := requireLevel(1, 0)(inner)
	req := httptest.NewRequest("PUT", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Error("inner handler should not be called in read-only mode")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403", w.Code)
	}
}

func TestRequireLevel_ExactMatch(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireLevel(2, 2)(inner)
	req := httptest.NewRequest("PUT", "/nodes/abc/availability", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler should be called when level exactly matches")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestRequireLevel -v`
Expected: FAIL — `requireLevel` does not exist

- [ ] **Step 3: Implement `requireLevel`**

Replace `internal/api/write_middleware.go`:

```go
package api

import (
	"net/http"
	"strconv"
)

// requireLevel returns middleware that blocks requests when the configured
// operations level is below the required level for this endpoint.
//
// Levels:
//   - 0: read-only (all writes blocked)
//   - 1: operational (scale, restart, rollback, image update, env/labels/resources/healthcheck patches)
//   - 2: impactful (node availability/labels, service mode/endpoint-mode, task removal)
func requireLevel(required, configured int) func(http.HandlerFunc) http.Handler {
	return func(next http.HandlerFunc) http.Handler {
		if configured >= required {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeProblem(w, r, http.StatusForbidden,
				"this operation requires operations level "+strconv.Itoa(required)+
					", but the server is configured at level "+strconv.Itoa(configured))
		})
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestRequireLevel -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/write_middleware.go internal/api/write_middleware_test.go
git commit -m "feat(api): replace requireWrite with tiered requireLevel middleware"
```

---

### Task 4: Thread operations level through router

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/router.go`
- Modify: `main.go`

- [ ] **Step 1: Add `operationsLevel` to `Handlers`**

In `internal/api/handlers.go`, add to the `Handlers` struct:

```go
operationsLevel int
```

Update `NewHandlers` signature to accept the level:

```go
func NewHandlers(
	c *cache.Cache,
	b *Broadcaster,
	dc DockerLogStreamer,
	sc DockerSystemClient,
	wc DockerWriteClient,
	ready <-chan struct{},
	promClient *PromClient,
	operationsLevel int,
) *Handlers {
```

And set it in the returned struct:

```go
operationsLevel: operationsLevel,
```

- [ ] **Step 2: Expose operations level in health endpoint**

In `HandleHealth`, add `"operationsLevel"` to the response map:

```go
func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"status":          "ok",
		"version":         version.Version,
		"commit":          version.Commit,
		"buildDate":       version.Date,
		"operationsLevel": h.operationsLevel,
	})
}
```

- [ ] **Step 3: Update `NewRouter` to use `requireLevel` with tiers**

Change the `NewRouter` signature to accept `operationsLevel int`:

```go
func NewRouter(h *Handlers, b *Broadcaster, metricsProxy *PrometheusProxy, spa http.Handler, openapiSpec []byte, scalarJS []byte, enablePprof bool, authProvider auth.Provider, operationsLevel int) http.Handler {
```

Create local helpers at the top of the function body:

```go
	tier1 := requireLevel(1, operationsLevel)
	tier2 := requireLevel(2, operationsLevel)
```

Then replace all `requireWrite(...)` calls with the appropriate tier:

**Tier 1 (operational):**
```go
mux.Handle("PUT /services/{id}/scale", tier1(h.HandleScaleService))
mux.Handle("PUT /services/{id}/image", tier1(h.HandleUpdateServiceImage))
mux.Handle("POST /services/{id}/rollback", tier1(h.HandleRollbackService))
mux.Handle("POST /services/{id}/restart", tier1(h.HandleRestartService))
mux.Handle("PATCH /services/{id}/env", tier1(h.HandlePatchServiceEnv))
mux.Handle("PATCH /services/{id}/labels", tier1(h.HandlePatchServiceLabels))
mux.Handle("PATCH /services/{id}/resources", tier1(h.HandlePatchServiceResources))
mux.Handle("PUT /services/{id}/healthcheck", tier1(h.HandlePutServiceHealthcheck))
mux.Handle("PATCH /services/{id}/healthcheck", tier1(h.HandlePatchServiceHealthcheck))
```

**Tier 2 (impactful):**
```go
mux.Handle("PUT /nodes/{id}/availability", tier2(h.HandleUpdateNodeAvailability))
mux.Handle("PATCH /nodes/{id}/labels", tier2(h.HandlePatchNodeLabels))
mux.Handle("PUT /services/{id}/mode", tier2(h.HandleUpdateServiceMode))
mux.Handle("PUT /services/{id}/endpoint-mode", tier2(h.HandleUpdateServiceEndpointMode))
mux.Handle("DELETE /tasks/{id}", tier2(h.HandleRemoveTask))
```

- [ ] **Step 4: Update `main.go`**

Update the `NewHandlers` call to pass `cfg.OperationsLevel`:

```go
handlers := api.NewHandlers(stateCache, broadcaster, dockerClient, dockerClient, dockerClient, watcher.Ready(), promClient, cfg.OperationsLevel)
```

Update the `NewRouter` call:

```go
router := api.NewRouter(handlers, broadcaster, metricsProxy, spa, openapiSpec, scalarJS, cfg.Pprof, authProvider, cfg.OperationsLevel)
```

Add a startup log line (near existing Prometheus/pprof log lines):

```go
slog.Info("operations level", "level", cfg.OperationsLevel)
```

- [ ] **Step 5: Fix all test compilation errors**

Both `NewHandlers` and `NewRouter` signatures changed. Every call site in tests needs updating.

**`NewHandlers` call sites** — add `, 2` (full access) as the last argument. Files with calls:
- `internal/api/handlers_test.go`
- `internal/api/write_handlers_test.go`
- `internal/api/loghandler_test.go`
- `internal/api/integration_test.go`
- `internal/api/openapi_test.go`
- `internal/api/topology_test.go`
- `internal/api/middleware_test.go`
- `internal/api/handlers_bench_test.go`
- `internal/api/metricsstream_test.go`
- `internal/api/prometheus_test.go`

Search: `grep -rn 'NewHandlers(' internal/api/*_test.go` to find all sites.

**`NewRouter` call sites** — add `, 2` as the last argument. Files with calls:
- `internal/api/integration_test.go`
- `internal/api/openapi_test.go`
- `internal/api/middleware_test.go`

Search: `grep -rn 'NewRouter(' internal/api/*_test.go` to find all sites.

Run: `go build ./...`
Expected: compiles cleanly

- [ ] **Step 6: Run all tests**

Run: `go test ./...`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers.go internal/api/router.go main.go
git add -u  # catch any test file fixes
git commit -m "feat: thread operations level through router, gate write endpoints by tier"
```

---

### Task 5: Integration-level test for operations level gating

**Files:**
- Modify: `internal/api/write_middleware_test.go`

- [ ] **Step 1: Write an integration-style test that exercises the full handler chain**

Add to `internal/api/write_middleware_test.go`:

```go
func TestRequireLevel_Integration_ScaleBlockedAtLevel0(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil, 0)
	handler := requireLevel(1, 0)(h.HandleScaleService)

	body := strings.NewReader(`{"replicas": 3}`)
	req := httptest.NewRequest("PUT", "/services/svc1/scale", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403 in read-only mode", w.Code)
	}
}

func TestRequireLevel_Integration_ScaleAllowedAtLevel1(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(1)
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	}
	c.SetService(svc)

	mock := &mockWriteClient{
		scaleServiceFn: func(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
			return svc, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, mock, closedReady(), nil, 1)
	handler := requireLevel(1, 1)(h.HandleScaleService)

	body := strings.NewReader(`{"replicas": 3}`)
	req := httptest.NewRequest("PUT", "/services/svc1/scale", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}
```

Note: `closedReady()` already exists in `handlers_test.go`. The `mockWriteClient` is defined in `write_handlers_test.go`. Both are available since tests are in the same package.

Add necessary imports: `"context"`, `"strings"`, `"github.com/docker/docker/api/types/swarm"`, `"github.com/radiergummi/cetacean/internal/cache"`.

- [ ] **Step 2: Run tests**

Run: `go test ./internal/api/ -run TestRequireLevel -v`
Expected: all PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/write_middleware_test.go
git commit -m "test(api): add integration tests for operations level gating"
```

---

### Task 6: Update documentation

**Files:**
- Modify: `CLAUDE.md` — add `CETACEAN_OPERATIONS_LEVEL` to env var table, document tiers in Key Conventions
- Modify: `docs/configuration.md` — add operations level section

- [ ] **Step 1: Add env var to CLAUDE.md table**

In the Environment variables table, add:

```
| `CETACEAN_OPERATIONS_LEVEL` | `1` | No (0=read-only, 1=operational, 2=impactful) |
```

- [ ] **Step 2: Add TOML example to `docs/configuration.md`**

Add a section explaining the operations level, the tier breakdown, and TOML example:

```toml
[server]
operations_level = 1  # 0=read-only, 1=operational, 2=impactful
```

- [ ] **Step 3: Update OpenAPI spec**

In `api/openapi.yaml`, add `operationsLevel` (integer) to the `/-/health` response schema.

- [ ] **Step 4: Update CHANGELOG.md**

Add under `[Unreleased]`:

```
### Added
- Operations level setting (`CETACEAN_OPERATIONS_LEVEL`) to gate write operations by danger tier
```

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md docs/configuration.md CHANGELOG.md api/openapi.yaml
git commit -m "docs: document operations level configuration"
```

---

### Task 7: Verify everything

- [ ] **Step 1: Run full test suite**

Run: `make check`
Expected: lint + fmt-check + all tests PASS

- [ ] **Step 2: Manual smoke test (optional)**

Run the server with `CETACEAN_OPERATIONS_LEVEL=0` and confirm write endpoints return 403. Run with `CETACEAN_OPERATIONS_LEVEL=2` and confirm all writes work.
