# Write Operations Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add targeted write operations (scale, image update, rollback, restart, drain, task removal) to the Cetacean dashboard, with inline UI actions and command palette integration.

**Architecture:** Action-oriented POST sub-resource routes on the existing REST API. Mutations go through Docker Engine API; the existing watcher→cache→SSE pipeline propagates updates to all clients. Frontend uses separate `post`/`del` helpers and waits for SSE confirmation rather than optimistic updates.

**Tech Stack:** Go stdlib `net/http`, Docker Engine SDK, React 19, TypeScript, Tailwind CSS, shadcn/ui

**Spec:** `docs/specs/2026-03-17-write-operations-design.md`

---

## Chunk 1: Backend Infrastructure + Scale Service

### Task 1: DockerWriteClient interface and ScaleService

**Files:**
- Modify: `internal/api/handlers.go:35-59` (add interface, extend Handlers struct + constructor)
- Modify: `internal/docker/client.go:30-46` (add ScaleService method)
- Create: `internal/api/write_handlers.go` (new file for all write handlers)
- Create: `internal/api/write_handlers_test.go` (tests)

- [ ] **Step 1: Define the DockerWriteClient interface and wire it into Handlers**

In `internal/api/handlers.go`, add the interface after `DockerSystemClient` (line 44):

```go
type DockerWriteClient interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
}
```

Add the field to `Handlers` struct (after line 50, the `systemClient` field):

```go
writeClient DockerWriteClient
```

Update `NewHandlers` to accept it:

```go
func NewHandlers(c *cache.Cache, b *Broadcaster, dc DockerLogStreamer, sc DockerSystemClient, wc DockerWriteClient, ready <-chan struct{}, promClient *PromClient) *Handlers {
	return &Handlers{cache: c, broadcaster: b, dockerClient: dc, systemClient: sc, writeClient: wc, ready: ready, promClient: promClient}
}
```

- [ ] **Step 2: Fix all callers of NewHandlers**

Update `main.go:185` — add `dockerClient` as the write client arg:

```go
handlers := api.NewHandlers(stateCache, broadcaster, dockerClient, dockerClient, dockerClient, watcher.Ready(), promClient)
```

Update **all** test files that call `NewHandlers` — there are 100+ call sites across these files:
- `internal/api/handlers_test.go` (~85 calls)
- `internal/api/handlers_bench_test.go` (~12 calls)
- `internal/api/loghandler_test.go` (~12 calls)
- `internal/api/topology_test.go` (5 calls)
- `internal/api/openapi_test.go` (1 call)
- `internal/api/middleware_test.go` (1 call)
- `internal/api/integration_test.go` (1 call)

Each call currently passes 6 args; add `nil` as the 5th arg (write client) since tests don't need it yet:

```go
NewHandlers(cache.New(nil), nil, nil, nil, nil, closedReady(), nil)
```

Use search-and-replace across all files to add the extra `nil` argument.

- [ ] **Step 3: Run tests to verify the refactor compiles**

Run: `go test ./internal/api/ -count=1`
Expected: All existing tests pass (this runs all tests, not just a subset)

- [ ] **Step 4: Implement ScaleService on the Docker client**

In `internal/docker/client.go`, add after the `Logs` method (after line 302):

```go
// ScaleService sets the replica count for a replicated-mode service.
// Returns the re-inspected service after the update.
func (c *Client) ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.Mode.Replicated == nil {
		return swarm.Service{}, fmt.Errorf("cannot scale a global-mode service")
	}
	svc.Spec.Mode.Replicated.Replicas = &replicas
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./...`
Expected: Clean build

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers.go internal/docker/client.go main.go internal/api/*_test.go
git commit -m "feat: add DockerWriteClient interface and ScaleService method"
```

### Task 2: requireWrite middleware

**Files:**
- Create: `internal/api/write_middleware.go`
- Create: `internal/api/write_middleware_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/api/write_middleware_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestRequireWrite_PassesThrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireWrite(inner)
	req := httptest.NewRequest("POST", "/services/abc/scale", nil)
	ctx := auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "user1"})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestRequireWrite_NoIdentity_PassesThrough(t *testing.T) {
	// With no identity in context (none auth mode pre-middleware),
	// requireWrite is a pass-through today — auth middleware upstream handles 401.
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireWrite(inner)
	req := httptest.NewRequest("POST", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler was not called — requireWrite should be a pass-through today")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -count=1 -run TestRequireWrite`
Expected: FAIL — `requireWrite` undefined

- [ ] **Step 3: Implement requireWrite**

Create `internal/api/write_middleware.go`:

```go
package api

import "net/http"

// requireWrite is a middleware placeholder for future RBAC on write operations.
// Today it is a pass-through: the auth middleware upstream already rejects
// unauthenticated requests before handlers run. This middleware will check
// identity.Groups against allowed roles once authorization is implemented.
func requireWrite(next http.HandlerFunc) http.Handler {
	return next
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -count=1 -run TestRequireWrite`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/write_middleware.go internal/api/write_middleware_test.go
git commit -m "feat: add requireWrite middleware scaffold for future RBAC"
```

### Task 3: HandleScaleService endpoint

**Files:**
- Create: `internal/api/write_handlers.go`
- Create: `internal/api/write_handlers_test.go`
- Modify: `internal/api/router.go:46-50` (add route)

- [ ] **Step 1: Write the failing test**

Create `internal/api/write_handlers_test.go`:

```go
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

type mockWriteClient struct {
	scaleServiceFn func(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
}

func (m *mockWriteClient) ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
	if m.scaleServiceFn != nil {
		return m.scaleServiceFn(ctx, id, replicas)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func TestHandleScaleService_OK(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		},
	})

	wc := &mockWriteClient{
		scaleServiceFn: func(_ context.Context, id string, r uint64) (swarm.Service, error) {
			newReplicas := r
			return swarm.Service{
				ID:   id,
				Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &newReplicas}}},
			}, nil
		},
	}

	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)
	body := `{"replicas": 5}`
	req := httptest.NewRequest("POST", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestHandleScaleService_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil)

	body := `{"replicas": 5}`
	req := httptest.NewRequest("POST", "/services/missing/scale", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleScaleService_GlobalMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc-global",
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}}},
	})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil)
	body := `{"replicas": 5}`
	req := httptest.NewRequest("POST", "/services/svc-global/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc-global")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleScaleService_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(3)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}}},
	})

	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil)
	req := httptest.NewRequest("POST", "/services/svc1/scale", strings.NewReader("not json"))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify tests fail**

Run: `go test ./internal/api/ -count=1 -run TestHandleScaleService`
Expected: FAIL — `HandleScaleService` undefined

- [ ] **Step 3: Implement HandleScaleService**

Create `internal/api/write_handlers.go`:

```go
package api

import (
	"log/slog"
	"net/http"

	"github.com/docker/docker/errdefs"
	json "github.com/goccy/go-json"
)

type scaleRequest struct {
	Replicas *uint64 `json:"replicas"`
}

func (h *Handlers) HandleScaleService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Replicas == nil {
		writeProblem(w, r, http.StatusBadRequest, "replicas is required")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		writeProblem(w, r, http.StatusBadRequest, "cannot scale a global-mode service")
		return
	}

	slog.Info("scaling service", "service", id, "replicas", *req.Replicas)

	updated, err := h.writeClient.ScaleService(r.Context(), id, *req.Replicas)
	if err != nil {
		if errdefs.IsNotFound(err) {
			writeProblem(w, r, http.StatusNotFound, "service not found")
			return
		}
		if errdefs.IsConflict(err) {
			writeProblem(w, r, http.StatusConflict, "service was modified by another client, please retry")
			return
		}
		slog.Error("failed to scale service", "service", id, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to scale service")
		return
	}

	// Use writeJSON (not writeJSONWithETag) for mutation responses:
	// ETag + If-None-Match → 304 is only valid for safe methods (GET/HEAD)
	// per RFC 9110 Section 13.1.1.
	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -count=1 -run TestHandleScaleService`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Register the route**

In `internal/api/router.go`, add after the service routes (after line 50, the `GET /services/{id}/logs` line):

```go
	// Service write operations
	mux.Handle("POST /services/{id}/scale", requireWrite(h.HandleScaleService))
```

- [ ] **Step 6: Run all tests to verify nothing broke**

Run: `go test ./internal/api/ -count=1`
Expected: All tests pass

- [ ] **Step 7: Commit**

```bash
git add internal/api/write_handlers.go internal/api/write_handlers_test.go internal/api/router.go
git commit -m "feat: add POST /services/{id}/scale endpoint"
```

### Task 4: Frontend mutation helpers

**Files:**
- Modify: `frontend/src/api/client.ts:31-54` (add post/del helpers)

- [ ] **Step 1: Add post and del helpers to client.ts**

In `frontend/src/api/client.ts`, add after the `fetchJSON` function (after line 54):

```typescript
async function post<T>(path: string, body?: unknown): Promise<T> {
  const h: Record<string, string> = { Accept: "application/json" };
  if (body !== undefined) h["Content-Type"] = "application/json";
  const res = await fetch(path, {
    method: "POST",
    headers: h,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    if (
      res.status === 401 &&
      res.headers.get("WWW-Authenticate")?.startsWith("Bearer")
    ) {
      const redirect = encodeURIComponent(
        window.location.pathname + window.location.search,
      );
      window.location.href = `/auth/login?redirect=${redirect}`;
      return new Promise<T>(() => {});
    }
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.detail) message = body.detail;
    } catch {
      // response wasn't JSON
    }
    throw new Error(message);
  }
  return res.json();
}

async function del(path: string): Promise<void> {
  const res = await fetch(path, {
    method: "DELETE",
    headers: { Accept: "application/json" },
  });
  if (!res.ok) {
    if (
      res.status === 401 &&
      res.headers.get("WWW-Authenticate")?.startsWith("Bearer")
    ) {
      const redirect = encodeURIComponent(
        window.location.pathname + window.location.search,
      );
      window.location.href = `/auth/login?redirect=${redirect}`;
      return new Promise<void>(() => {});
    }
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.detail) message = body.detail;
    } catch {}
    throw new Error(message);
  }
}
```

- [ ] **Step 2: Add scaleService to the api object**

In the `api` object (around line 138), add at the end before the closing `}`:

```typescript
  scaleService: (id: string, replicas: number) =>
    post<ServiceDetail>(`/services/${id}/scale`, { replicas }),
```

- [ ] **Step 3: Verify frontend compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat: add post/del mutation helpers and scaleService API method"
```

### Task 5: Scale action on service detail page

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add scale popover to the service detail page**

This task requires reading the full `ServiceDetail.tsx` to understand where to place the UI. The scale button should appear near the replica count display.

Add a `ScalePopover` component and integrate it into the service detail page:

1. Add state for the scale action:
```typescript
const [scaleOpen, setScaleOpen] = useState(false);
const [scaleReplicas, setScaleReplicas] = useState("");
const [scaleLoading, setScaleLoading] = useState(false);
const [scaleError, setScaleError] = useState<string | null>(null);
```

2. Add the scale handler:
```typescript
const handleScale = useCallback(async () => {
  if (!id || !scaleReplicas) return;
  const n = parseInt(scaleReplicas, 10);
  if (isNaN(n) || n < 0) {
    setScaleError("Invalid replica count");
    return;
  }
  setScaleLoading(true);
  setScaleError(null);
  try {
    await api.scaleService(id, n);
    setScaleOpen(false);
    setScaleReplicas("");
  } catch (err) {
    setScaleError(err instanceof Error ? err.message : "Failed to scale");
  } finally {
    setScaleLoading(false);
  }
}, [id, scaleReplicas]);
```

3. Add a scale button near the replica count in the detail UI. The exact placement depends on the current page layout — read the full file to find where replicas are displayed and add the button adjacent to it.

The popover should be a small inline form: number input pre-filled with the current replica count, a "Scale" confirm button, and a cancel button. Use existing shadcn/ui `Popover` if available, otherwise a simple absolutely-positioned div.

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: No errors

- [ ] **Step 3: Verify lint passes**

Run: `cd frontend && npm run lint`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: add scale action button on service detail page"
```

### Task 6: Update CLAUDE.md and OpenAPI spec

**Files:**
- Modify: `CLAUDE.md` (update "All API endpoints are GET-only" convention)
- Modify: `api/openapi.yaml` (add POST /services/{id}/scale)

- [ ] **Step 1: Update CLAUDE.md**

Find the line "All API endpoints are GET-only (read-only system)" and update it to reflect that write operations now exist. Also add a note about the `DockerWriteClient` interface to the Backend architecture section.

- [ ] **Step 2: Add the scale endpoint to openapi.yaml**

Add a `POST /services/{id}/scale` path with request body schema (`{ replicas: integer }`) and response schema (ServiceDetail wrapper). Include 400, 404, 409 error responses.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md api/openapi.yaml
git commit -m "docs: update CLAUDE.md and OpenAPI spec for write operations"
```

---

## Chunk 2: Remaining Service Operations (Image, Rollback, Restart)

### Task 7: UpdateServiceImage

**Files:**
- Modify: `internal/api/handlers.go:39-44` (extend DockerWriteClient interface)
- Modify: `internal/docker/client.go` (add UpdateServiceImage method)
- Modify: `internal/api/write_handlers.go` (add handler)
- Modify: `internal/api/write_handlers_test.go` (add tests)
- Modify: `internal/api/router.go` (add route)

- [ ] **Step 1: Add to interface**

Add to `DockerWriteClient`:

```go
UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error)
```

- [ ] **Step 2: Write failing tests**

Add to `write_handlers_test.go`:

```go
func (m *mockWriteClient) UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error) {
	if m.updateServiceImageFn != nil {
		return m.updateServiceImageFn(ctx, id, image)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}
```

Add `updateServiceImageFn` field to `mockWriteClient`.

Tests: OK case (200), not found (404), empty image (400).

- [ ] **Step 3: Implement Docker client method**

In `internal/docker/client.go`:

```go
func (c *Client) UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.ContainerSpec.Image = image
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 4: Implement handler**

In `write_handlers.go`:

```go
type imageRequest struct {
	Image string `json:"image"`
}

func (h *Handlers) HandleUpdateServiceImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req imageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Image == "" {
		writeProblem(w, r, http.StatusBadRequest, "image is required")
		return
	}

	if _, ok := h.cache.GetService(id); !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("updating service image", "service", id, "image", req.Image)

	updated, err := h.writeClient.UpdateServiceImage(r.Context(), id, req.Image)
	if err != nil {
		if errdefs.IsNotFound(err) {
			writeProblem(w, r, http.StatusNotFound, "service not found")
			return
		}
		if errdefs.IsConflict(err) {
			writeProblem(w, r, http.StatusConflict, "service was modified, please retry")
			return
		}
		slog.Error("failed to update service image", "service", id, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to update service image")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}
```

- [ ] **Step 5: Register route**

In `router.go`, after the scale route:

```go
	mux.Handle("POST /services/{id}/image", requireWrite(h.HandleUpdateServiceImage))
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/api/ -count=1`
Expected: All pass

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers.go internal/docker/client.go internal/api/write_handlers.go internal/api/write_handlers_test.go internal/api/router.go
git commit -m "feat: add POST /services/{id}/image endpoint"
```

### Task 8: RollbackService

**Files:** Same pattern as Task 7.

- [ ] **Step 1: Add to interface**

```go
RollbackService(ctx context.Context, id string) (swarm.Service, error)
```

- [ ] **Step 2: Write failing tests**

Tests: OK case (200), not found (404), service with no previous spec (400 — check `svc.PreviousSpec == nil`).

- [ ] **Step 3: Implement Docker client method**

```go
func (c *Client) RollbackService(ctx context.Context, id string) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.PreviousSpec == nil {
		return swarm.Service{}, fmt.Errorf("service has no previous spec to rollback to")
	}
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{
		Rollback: "previous",
	})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 4: Implement handler**

No request body — the handler must NOT call `json.NewDecoder(r.Body).Decode(...)`. It should go straight from cache lookup to `h.writeClient.RollbackService(...)`. Check `svc.PreviousSpec == nil` in the handler (from cache) and return 400 before calling the Docker client. Use `writeJSON` (not `writeJSONWithETag`) for the response.

- [ ] **Step 5: Register route**

```go
	mux.Handle("POST /services/{id}/rollback", requireWrite(h.HandleRollbackService))
```

- [ ] **Step 6: Run tests, commit**

```bash
git commit -m "feat: add POST /services/{id}/rollback endpoint"
```

### Task 9: RestartService (force update)

**Files:** Same pattern.

- [ ] **Step 1: Add to interface**

```go
RestartService(ctx context.Context, id string) (swarm.Service, error)
```

- [ ] **Step 2: Write failing tests**

Tests: OK case (200), not found (404).

- [ ] **Step 3: Implement Docker client method**

```go
func (c *Client) RestartService(ctx context.Context, id string) (swarm.Service, error) {
	svc, _, err := c.docker.ServiceInspectWithRaw(ctx, id, swarm.ServiceInspectOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	svc.Spec.TaskTemplate.ForceUpdate++
	_, err = c.docker.ServiceUpdate(ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{})
	if err != nil {
		return swarm.Service{}, err
	}
	return c.InspectService(ctx, id)
}
```

- [ ] **Step 4: Implement handler**

No request body — same as rollback, do NOT decode body. Go from cache lookup straight to `h.writeClient.RestartService(...)`. Use `writeJSON` for the response.

- [ ] **Step 5: Register route**

```go
	mux.Handle("POST /services/{id}/restart", requireWrite(h.HandleRestartService))
```

- [ ] **Step 6: Run tests, commit**

```bash
git commit -m "feat: add POST /services/{id}/restart endpoint"
```

### Task 10: Frontend — image, rollback, restart actions on service detail

**Files:**
- Modify: `frontend/src/api/client.ts` (add methods)
- Modify: `frontend/src/pages/ServiceDetail.tsx` (add buttons)

- [ ] **Step 1: Add API methods**

In the `api` object in `client.ts`:

```typescript
  updateServiceImage: (id: string, image: string) =>
    post<ServiceDetail>(`/services/${id}/image`, { image }),
  rollbackService: (id: string) =>
    post<ServiceDetail>(`/services/${id}/rollback`),
  restartService: (id: string) =>
    post<ServiceDetail>(`/services/${id}/restart`),
```

- [ ] **Step 2: Add action buttons to service detail page**

Add "Update Image" popover (text input for image), "Rollback" button (confirmation dialog), and "Restart" button (confirmation dialog). Follow the same pattern established in Task 5.

Rollback button should be disabled when `service.PreviousSpec` is null/undefined.

- [ ] **Step 3: Verify it compiles and lints**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: add image update, rollback, and restart actions on service detail"
```

---

## Chunk 3: Node Operations + Task Removal

### Task 11: UpdateNodeAvailability

**Files:**
- Modify: `internal/api/handlers.go` (extend DockerWriteClient)
- Modify: `internal/docker/client.go` (add method)
- Modify: `internal/api/write_handlers.go` (add handler)
- Modify: `internal/api/write_handlers_test.go` (add tests)
- Modify: `internal/api/router.go` (add route)

- [ ] **Step 1: Add to interface**

```go
UpdateNodeAvailability(ctx context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error)
```

- [ ] **Step 2: Write failing tests**

Tests: OK/drain (200), not found (404), invalid availability value (400).

- [ ] **Step 3: Implement Docker client method**

```go
func (c *Client) UpdateNodeAvailability(ctx context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error) {
	node, _, err := c.docker.NodeInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Node{}, err
	}
	node.Spec.Availability = availability
	err = c.docker.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
	if err != nil {
		return swarm.Node{}, err
	}
	return c.InspectNode(ctx, id)
}
```

- [ ] **Step 4: Implement handler**

Validate that `availability` is one of `"active"`, `"drain"`, `"pause"`. Map string to `swarm.NodeAvailabilityActive` / `swarm.NodeAvailabilityDrain` / `swarm.NodeAvailabilityPause`.

- [ ] **Step 5: Register route**

```go
	mux.Handle("POST /nodes/{id}/availability", requireWrite(h.HandleUpdateNodeAvailability))
```

- [ ] **Step 6: Run tests, commit**

```bash
git commit -m "feat: add POST /nodes/{id}/availability endpoint"
```

### Task 12: RemoveTask (force-kill backing container)

**Files:** Same pattern.

- [ ] **Step 1: Add to interface**

```go
RemoveTask(ctx context.Context, id string) error
```

- [ ] **Step 2: Write failing tests**

Tests: OK (204), task not found (404), task with no running container (404 — the container resource doesn't exist).

- [ ] **Step 3: Implement Docker client method**

```go
func (c *Client) RemoveTask(ctx context.Context, id string) error {
	task, _, err := c.docker.TaskInspectWithRaw(ctx, id)
	if err != nil {
		return err
	}
	containerID := task.Status.ContainerStatus.ContainerID
	if containerID == "" {
		return errdefs.NotFound(fmt.Errorf("task has no running container"))
	}
	return c.docker.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}
```

- [ ] **Step 4: Implement handler**

Handler returns 204 on success (use `w.WriteHeader(http.StatusNoContent)`), no response body.

- [ ] **Step 5: Register route**

```go
	mux.Handle("DELETE /tasks/{id}", requireWrite(h.HandleRemoveTask))
```

- [ ] **Step 6: Run tests, commit**

```bash
git commit -m "feat: add DELETE /tasks/{id} endpoint (force-remove via container kill)"
```

### Task 13: Frontend — node availability + task removal

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/pages/NodeDetail.tsx`
- Modify: `frontend/src/pages/TaskDetail.tsx`

- [ ] **Step 1: Add API methods**

```typescript
  updateNodeAvailability: (id: string, availability: "active" | "drain" | "pause") =>
    post<{ node: Node }>(`/nodes/${id}/availability`, { availability }),
  removeTask: (id: string) => del(`/tasks/${id}`),
```

- [ ] **Step 2: Add availability dropdown on node detail page**

A dropdown/select with three options: Active, Drain, Pause. Changing to Drain shows a confirmation dialog. Current availability is pre-selected.

- [ ] **Step 3: Add "Force Remove" button on task detail page**

Button with confirmation modal: "Are you sure you want to force-remove this task? This will kill the backing container."

- [ ] **Step 4: Verify compiles and lints**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: add node availability control and task force-remove UI"
```

---

## Chunk 4: Command Palette Actions

### Task 14: Action registry

**Files:**
- Create: `frontend/src/lib/actions.ts` (action definitions and registry)

- [ ] **Step 1: Create the action registry module**

```typescript
import type { Node, ServiceListItem, Task } from "../api/types";

export interface PaletteAction {
  id: string;
  label: string;
  keywords: string[];
  steps: PaletteStep[];
  execute: (...args: any[]) => Promise<void>;
  destructive?: boolean;
}

export interface PaletteStep {
  type: "resource" | "number" | "text" | "choice";
  resourceType?: string;
  label: string;
  placeholder?: string;
  choices?: { label: string; value: string }[];
}

export function getActions(api: typeof import("../api/client").api): PaletteAction[] {
  return [
    {
      id: "scale",
      label: "Scale Service",
      keywords: ["scale", "replicas"],
      steps: [
        { type: "resource", resourceType: "service", label: "Service" },
        { type: "number", label: "Replicas", placeholder: "Number of replicas" },
      ],
      execute: async (service: ServiceListItem, replicas: number) => {
        await api.scaleService(service.ID, replicas);
      },
    },
    {
      id: "image",
      label: "Update Image",
      keywords: ["image", "deploy", "tag"],
      steps: [
        { type: "resource", resourceType: "service", label: "Service" },
        { type: "text", label: "Image", placeholder: "e.g. nginx:1.27" },
      ],
      execute: async (service: ServiceListItem, image: string) => {
        await api.updateServiceImage(service.ID, image);
      },
    },
    {
      id: "rollback",
      label: "Rollback Service",
      keywords: ["rollback", "revert"],
      steps: [{ type: "resource", resourceType: "service", label: "Service" }],
      destructive: true,
      execute: async (service: ServiceListItem) => {
        await api.rollbackService(service.ID);
      },
    },
    {
      id: "restart",
      label: "Restart Service",
      keywords: ["restart", "redeploy"],
      steps: [{ type: "resource", resourceType: "service", label: "Service" }],
      destructive: true,
      execute: async (service: ServiceListItem) => {
        await api.restartService(service.ID);
      },
    },
    {
      id: "drain",
      label: "Drain Node",
      keywords: ["drain"],
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      destructive: true,
      execute: async (node: Node) => {
        await api.updateNodeAvailability(node.ID, "drain");
      },
    },
    {
      id: "activate",
      label: "Activate Node",
      keywords: ["activate", "undrain"],
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      execute: async (node: Node) => {
        await api.updateNodeAvailability(node.ID, "active");
      },
    },
    {
      id: "pause",
      label: "Pause Node",
      keywords: ["pause"],
      steps: [{ type: "resource", resourceType: "node", label: "Node" }],
      destructive: true,
      execute: async (node: Node) => {
        await api.updateNodeAvailability(node.ID, "pause");
      },
    },
    {
      id: "remove-task",
      label: "Force Remove Task",
      keywords: ["remove", "kill", "task"],
      steps: [{ type: "resource", resourceType: "task", label: "Task" }],
      destructive: true,
      execute: async (task: Task) => {
        await api.removeTask(task.ID);
      },
    },
  ];
}
```

- [ ] **Step 2: Add fuzzy matching helper**

```typescript
export function matchAction(input: string, actions: PaletteAction[]): { action: PaletteAction; remainder: string } | null {
  const lower = input.toLowerCase().trim();
  for (const action of actions) {
    for (const keyword of action.keywords) {
      if (lower.startsWith(keyword)) {
        const remainder = lower.slice(keyword.length).trim();
        return { action, remainder };
      }
    }
  }
  return null;
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/actions.ts
git commit -m "feat: add command palette action registry with fuzzy matching"
```

### Task 15: Integrate actions into SearchPalette

**Files:**
- Modify: `frontend/src/components/search/SearchPalette.tsx`

- [ ] **Step 1: Extend SearchPalette with action mode**

This is the most complex frontend task. The palette needs a second mode:

1. When input starts with a keyword matching an action, switch to "action mode"
2. In action mode, show the matched action and filter resources by the remainder text
3. After selecting a resource, advance to the next step (number/text input or confirmation)
4. On final step, execute and close

The implementation should:
- Import `getActions`, `matchAction` from `@/lib/actions`
- Add state: `actionMode: PaletteAction | null`, `actionStep: number`, `actionArgs: any[]`
- When `matchAction(query)` returns a hit, show action results instead of search results
- Resource picker step: use existing `api.search` to find resources filtered by type
- Number/text step: show a simple input
- Destructive actions: show a confirmation step before executing

Read the full `SearchPalette.tsx` before implementing to understand the keyboard navigation, portal rendering, and styling patterns.

- [ ] **Step 2: Verify it compiles and lints**

Run: `cd frontend && npx tsc -b --noEmit && npm run lint`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/search/SearchPalette.tsx
git commit -m "feat: integrate write actions into command palette"
```

---

## Chunk 5: Tier 2 Operations (Node Labels, Service Env, Service Resources)

### Task 16: UpdateNodeLabels

**Files:** Same backend pattern as previous tasks.

- [ ] **Step 1: Add to interface**

```go
UpdateNodeLabels(ctx context.Context, id string, set map[string]string, remove []string) (swarm.Node, error)
```

- [ ] **Step 2: Implement Docker client, handler, tests, route**

Route: `POST /nodes/{id}/labels`
Body: `{"set": {"key": "val"}, "remove": ["key2"]}`

- [ ] **Step 3: Frontend API method + labels edit UI on node detail page**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: add POST /nodes/{id}/labels endpoint and UI"
```

### Task 17: UpdateServiceEnv

- [ ] **Step 1: Add to interface**

```go
UpdateServiceEnv(ctx context.Context, id string, set map[string]string, remove []string) (swarm.Service, error)
```

- [ ] **Step 2: Implement**

Docker stores env vars as `[]string` in format `KEY=VALUE` on `svc.Spec.TaskTemplate.ContainerSpec.Env`. The method must merge `set` values and remove `remove` keys from this slice.

- [ ] **Step 3: Handler, tests, route**

Route: `POST /services/{id}/env`

- [ ] **Step 4: Frontend API method + UI**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: add POST /services/{id}/env endpoint and UI"
```

### Task 18: UpdateServiceResources

- [ ] **Step 1: Add to interface**

```go
UpdateServiceResources(ctx context.Context, id string, limits, reservations *swarm.Resources) (swarm.Service, error)
```

- [ ] **Step 2: Implement**

Updates `svc.Spec.TaskTemplate.Resources.Limits` and/or `svc.Spec.TaskTemplate.Resources.Reservations`. Either field can be nil (meaning "don't change").

- [ ] **Step 3: Handler, tests, route**

Route: `POST /services/{id}/resources`

- [ ] **Step 4: Frontend API method + UI**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: add POST /services/{id}/resources endpoint and UI"
```

### Task 19: Final OpenAPI + CLAUDE.md update

- [ ] **Step 1: Add all remaining endpoints to openapi.yaml**
- [ ] **Step 2: Update CLAUDE.md with complete write operation documentation**
- [ ] **Step 3: Commit**

```bash
git commit -m "docs: complete OpenAPI spec and CLAUDE.md for all write operations"
```
