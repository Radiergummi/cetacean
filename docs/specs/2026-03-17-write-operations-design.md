# Write Operations for Cetacean

**Date:** 2026-03-17
**Status:** Approved

## Goal

Add careful write operations to Cetacean's read-only Docker Swarm dashboard. The scope covers operational triage (
draining nodes, scaling services, removing stuck tasks) and service tuning (updating images, rollback, restart, env
vars, resource limits). Deploying new services or stacks is explicitly out of scope.

## Constraints

- No service/stack creation — only mutations to existing resources
- Authorization is deferred: authenticated = authorized for now, with the architecture ready for role-based
  authorization later
- All mutations go through the Docker API; the existing watcher/cache/SSE pipeline handles propagation to all clients
- No optimistic UI updates — wait for SSE confirmation

## HTTP Method Semantics

Each write endpoint uses the HTTP method that matches its semantics per RFC 9110:

- **PUT** — idempotent full replacement of a sub-resource's state. Safe to retry on network failure.
- **POST** — non-idempotent actions (rollback, restart). Not safe to blindly retry.
- **PATCH** — partial modification of a sub-resource. Uses RFC 6902 (JSON Patch) for flat key-value maps (env, labels) and RFC 7396 (JSON Merge Patch) for structured sub-resources (resource limits).
- **DELETE** — resource removal.

## Operations

### Tier 1 — High-frequency operational actions

| Operation             | Method   | Route                           | Content-Type                     | Request Body                | Response              |
|-----------------------|----------|---------------------------------|----------------------------------|-----------------------------|-----------------------|
| Scale service         | `PUT`    | `/services/{id}/scale`          | `application/json`               | `{"replicas": 5}`           | 200 + updated service |
| Update service image  | `PUT`    | `/services/{id}/image`          | `application/json`               | `{"image": "nginx:1.27"}`   | 200 + updated service |
| Rollback service      | `POST`   | `/services/{id}/rollback`       | (none)                           | (empty)                     | 200 + updated service |
| Restart service       | `POST`   | `/services/{id}/restart`        | (none)                           | (empty)                     | 200 + updated service |
| Set node availability | `PUT`    | `/nodes/{id}/availability`      | `application/json`               | `{"availability": "drain"}` | 200 + updated node    |
| Force-remove task     | `DELETE` | `/tasks/{id}`                   | (none)                           | (none)                      | 204                   |

**Note on task removal:** Docker Swarm has no direct task removal API. "Force-remove task" is implemented by inspecting
the task's `Status.ContainerStatus.ContainerID` and calling `ContainerRemove` with `Force: true`. This kills the backing
container; the swarm scheduler then reconciles (rescheduling if the service still demands replicas). If the task has no
container (e.g., already exited), the endpoint returns 404.

**Note on idempotency:** Rollback and restart are not idempotent — rollback swaps current ↔ previous (calling it twice
undoes the rollback), and restart increments `ForceUpdate` (each call triggers a new rolling restart). These use POST
to signal that clients must not blindly retry on failure. The 409 error message for these endpoints says "please refresh
and retry" rather than "please retry."

### Tier 2 — Sub-resource PATCH endpoints

Tier 2 endpoints treat env vars, labels, and resource limits as sub-resources with their own GET and PATCH methods.

| Operation                | Method  | Route                           | Content-Type                      | Response              |
|--------------------------|---------|--------------------------------|-----------------------------------|-----------------------|
| Read service env         | `GET`   | `/services/{id}/env`            | —                                 | 200 + env map         |
| Update service env       | `PATCH` | `/services/{id}/env`            | `application/json-patch+json`     | 200 + updated env map |
| Read node labels         | `GET`   | `/nodes/{id}/labels`            | —                                 | 200 + labels map      |
| Update node labels       | `PATCH` | `/nodes/{id}/labels`            | `application/json-patch+json`     | 200 + updated labels  |
| Read service resources   | `GET`   | `/services/{id}/resources`      | —                                 | 200 + resources       |
| Update service resources | `PATCH` | `/services/{id}/resources`      | `application/merge-patch+json`    | 200 + updated resources |

#### JSON Patch (RFC 6902) for env vars and labels

The env and labels sub-resources are flat key-value maps. JSON Patch operations use the key as the path:

```
GET /services/{id}/env
→ {"FOO": "bar", "PORT": "8080", "DEBUG": "true"}

PATCH /services/{id}/env
Content-Type: application/json-patch+json

[
  {"op": "add", "path": "/NEW_VAR", "value": "hello"},
  {"op": "replace", "path": "/FOO", "value": "updated"},
  {"op": "remove", "path": "/DEBUG"}
]

→ {"FOO": "updated", "PORT": "8080", "NEW_VAR": "hello"}
```

**Path format:** Per RFC 6902, paths use JSON Pointer (RFC 6901) syntax with a leading `/`. For convenience, the server
also accepts paths without the leading slash (e.g., `"path": "FOO"` is treated as `"path": "/FOO"`). This avoids a
common source of 400 errors for clients interacting with flat maps.

**Supported operations:** `add`, `remove`, `replace`, `test`. The `move` and `copy` operations return 400 — they don't
have meaningful semantics for env vars or labels. The `test` operation enables conditional updates (field-level CAS)
without a separate optimistic concurrency mechanism:

```json
[
  {"op": "test", "path": "/FOO", "value": "bar"},
  {"op": "replace", "path": "/FOO", "value": "baz"}
]
```

Fails atomically with 409 if FOO is not "bar".

**Env var representation:** Docker stores env vars as `[]string` in `KEY=VALUE` format. The sub-resource endpoint
abstracts this into a `map[string]string` for both GET and PATCH. The server handles the `KEY=VALUE` array
transformation internally.

#### JSON Merge Patch (RFC 7396) for resources

The resources sub-resource is a small nested object. JSON Merge Patch is a natural fit — send the fields you want to
change, omit those you don't:

```
GET /services/{id}/resources
→ {"limits": {"NanoCPUs": 1000000000, "MemoryBytes": 536870912}, "reservations": {"NanoCPUs": 500000000}}

PATCH /services/{id}/resources
Content-Type: application/merge-patch+json

{"limits": {"MemoryBytes": 1073741824}}

→ {"limits": {"NanoCPUs": 1000000000, "MemoryBytes": 1073741824}, "reservations": {"NanoCPUs": 500000000}}
```

Set a field to `null` to remove it (per RFC 7396).

## Backend Design

### Docker Client

Add individual methods to `internal/docker/client.go` per operation. Each method:

1. Fetches the current resource (to get the version for optimistic concurrency)
2. Validates preconditions (e.g., service is replicated mode for scale)
3. Applies the targeted mutation to the spec
4. Calls the Docker API update method
5. Returns the updated resource or error

Example:

```go
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
	// Re-inspect to get authoritative server state
	return c.InspectService(ctx, id)
}
```

For task removal, the method inspects the task's `Status.ContainerStatus.ContainerID` and calls `ContainerRemove` with
`Force: true`.

**Validation rules:**

- `ScaleService`: reject if service is global-mode (return error, handler maps to 400)
- `UpdateServiceImage`: reject empty image string
- `UpdateNodeAvailability`: reject values other than "active", "drain", "pause"

For rollback, use `ServiceUpdate` with `Rollback: "previous"` in the update options.
For restart (force update), increment `ForceUpdate` on the task template without changing the spec.

### Routes

```go
// Tier 1: idempotent full-replacement (PUT)
mux.Handle("PUT /services/{id}/scale", requireWrite(h.HandleScaleService))
mux.Handle("PUT /services/{id}/image", requireWrite(h.HandleUpdateServiceImage))
mux.Handle("PUT /nodes/{id}/availability", requireWrite(h.HandleUpdateNodeAvailability))

// Tier 1: non-idempotent actions (POST)
mux.Handle("POST /services/{id}/rollback", requireWrite(h.HandleRollbackService))
mux.Handle("POST /services/{id}/restart", requireWrite(h.HandleRestartService))

// Tier 1: deletion
mux.Handle("DELETE /tasks/{id}", requireWrite(h.HandleRemoveTask))

// Tier 2: sub-resource reads (GET)
mux.HandleFunc("GET /services/{id}/env", contentNegotiated(h.HandleGetServiceEnv, spa))
mux.HandleFunc("GET /services/{id}/resources", contentNegotiated(h.HandleGetServiceResources, spa))
mux.HandleFunc("GET /nodes/{id}/labels", contentNegotiated(h.HandleGetNodeLabels, spa))

// Tier 2: partial updates (PATCH)
mux.Handle("PATCH /services/{id}/env", requireWrite(h.HandlePatchServiceEnv))
mux.Handle("PATCH /services/{id}/resources", requireWrite(h.HandlePatchServiceResources))
mux.Handle("PATCH /nodes/{id}/labels", requireWrite(h.HandlePatchNodeLabels))
```

**Handler injection:** The `Handlers` struct gains a new `DockerWriteClient` interface field:

```go
type DockerWriteClient interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error)
	RollbackService(ctx context.Context, id string) (swarm.Service, error)
	RestartService(ctx context.Context, id string) (swarm.Service, error)
	UpdateNodeAvailability(ctx context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error)
	RemoveTask(ctx context.Context, id string) error
	// Tier 2 methods added as implemented
}
```

This is wired in `main.go` alongside the existing `DockerLogStreamer` and `DockerSystemClient`. The interface keeps
handlers testable via mocks.

**Error detection:** Use `errdefs.IsConflict(err)` and `errdefs.IsNotFound(err)` from `github.com/docker/docker/errdefs`
to translate Docker errors to appropriate HTTP status codes (409, 404). All other Docker errors map to 500.

### Write Authorization Middleware

New `requireWrite` middleware in `internal/api/`:

```go
func requireWrite(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Today: the auth middleware upstream already rejects unauthenticated
		// requests (returning 401/redirect before handlers run). This middleware
		// is scaffolding for future RBAC — it will check identity.Groups against
		// allowed roles. For now it's a pass-through.
		//
		// Future: identity := auth.IdentityFromContext(r.Context())
		//         if !identity.HasRole("writer") { ... }
		next(w, r)
	})
}
```

When auth mode is `none`, the `NoneProvider` injects an anonymous identity upstream, so writes are allowed in
trusted/single-user environments. The auth exemption paths (`/-/*`, `/api*`, `/assets/*`, `/auth/*`) do NOT cover write
routes — all write endpoints go through the auth middleware.

### Response Patterns

- **Success**: 200 with JSON response, or 204 for deletions
- **Version conflict**: 409 with RFC 9457 problem detail
- **Not found**: 404 with problem detail
- **Validation error**: 400 with problem detail (e.g., replicas < 0, empty image string, unsupported JSON Patch op)
- **Patch test failure**: 409 with problem detail ("test operation failed: expected X, got Y")
- **In use**: 409 with problem detail (for future delete operations — "secret is used by services X, Y")
- **Wrong Content-Type**: 415 Unsupported Media Type (e.g., PATCH without `application/json-patch+json`)

**Response format for mutation endpoints:**
- Tier 1 PUT/POST: JSON-LD wrapped parent resource (same format as detail GET), using `writeJSON` (not
  `writeJSONWithETag` — ETags with conditional 304 are only valid for safe methods per RFC 9110 §13.1.1)
- Tier 2 GET: JSON-LD wrapped sub-resource with ETag (these are safe methods, so conditional responses are valid)
- Tier 2 PATCH: the updated sub-resource map/object (same format as GET response)
- DELETE: 204 No Content, empty body

**Content negotiation:** Write routes (PUT/POST/PATCH/DELETE) bypass the `contentNegotiated`/`contentNegotiatedWithSSE`
dispatch wrappers. The `negotiate` middleware still runs (it's in the global chain) but write handlers always respond
with JSON regardless of the negotiated content type. This is intentional — write endpoints are API-only, never served as
HTML. Tier 2 GET routes use `contentNegotiated` like other read endpoints.

**Content-Type validation for PATCH:** The handler must check `r.Header.Get("Content-Type")` and return 415 Unsupported
Media Type if it doesn't match the expected patch format. This prevents silent misinterpretation of request bodies.

### Data Flow

Mutations do NOT write to the cache directly. The flow is:

```
Handler → Docker Client → Docker Engine API
                              ↓
                         Docker Events
                              ↓
                    Watcher (event stream)
                              ↓
                      Cache (SetService, etc.)
                              ↓
                    SSE Broadcaster → All Clients
```

This means the mutating client sees the update via SSE just like every other client. No special-case cache invalidation
needed.

## Frontend Design

### Mutation Client

Three new helpers in `frontend/src/api/client.ts`:

```typescript
async function put<T>(path: string, body: unknown): Promise<T> {
    const res = await fetch(path, {
        method: "PUT",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify(body),
    });
    if (!res.ok) {
        // ... same error handling as fetchJSON (401 redirect, problem detail extraction)
    }
    return res.json();
}

async function post<T>(path: string): Promise<T> {
    const res = await fetch(path, {
        method: "POST",
        headers: { Accept: "application/json" },
    });
    if (!res.ok) { /* ... */ }
    return res.json();
}

async function patch<T>(path: string, ops: object[], contentType: string): Promise<T> {
    const res = await fetch(path, {
        method: "PATCH",
        headers: { "Content-Type": contentType, Accept: "application/json" },
        body: JSON.stringify(ops),
    });
    if (!res.ok) { /* ... */ }
    return res.json();
}

async function del(path: string): Promise<void> {
    const res = await fetch(path, {
        method: "DELETE",
        headers: { Accept: "application/json" },
    });
    if (!res.ok) { /* ... */ }
}
```

Per-operation typed functions:

```typescript
// Tier 1: PUT (idempotent)
api.scaleService(id, replicas)              // put(`/services/${id}/scale`, { replicas })
api.updateServiceImage(id, image)           // put(`/services/${id}/image`, { image })
api.updateNodeAvailability(id, availability) // put(`/nodes/${id}/availability`, { availability })

// Tier 1: POST (non-idempotent)
api.rollbackService(id)                     // post(`/services/${id}/rollback`)
api.restartService(id)                      // post(`/services/${id}/restart`)

// Tier 1: DELETE
api.removeTask(id)                          // del(`/tasks/${id}`)

// Tier 2: GET
api.serviceEnv(id)                          // fetchJSON(`/services/${id}/env`)
api.nodeLabels(id)                          // fetchJSON(`/nodes/${id}/labels`)
api.serviceResources(id)                    // fetchJSON(`/services/${id}/resources`)

// Tier 2: PATCH (RFC 6902 JSON Patch)
api.patchServiceEnv(id, ops)                // patch(`/services/${id}/env`, ops, "application/json-patch+json")
api.patchNodeLabels(id, ops)                // patch(`/nodes/${id}/labels`, ops, "application/json-patch+json")

// Tier 2: PATCH (RFC 7396 JSON Merge Patch)
api.patchServiceResources(id, partial)      // patch(`/services/${id}/resources`, partial, "application/merge-patch+json")
```

### Inline Actions

**Service detail page:**

- Scale button near replica count → popover with number input + confirm
- Update Image button → popover with image text input (shows current image)
- Rollback button → confirmation dialog
- Restart button → confirmation dialog
- Tier 2: Environment and Resources buttons

**Node detail page:**

- Availability control in header area → dropdown (active/drain/pause) with confirmation for drain
- Tier 2: Labels edit button

**Task detail page:**

- Force Remove button → confirmation dialog ("Are you sure you want to force-remove this task?")

**Confirmation tiers:**

- Non-destructive (scale, image update): inline popover, confirm button
- Disruptive (rollback, restart, drain): modal dialog with "Are you sure?"
- Destructive (force-remove task): modal dialog

**Loading state:**
Action buttons show a spinner between submission and the SSE event arriving with the updated resource. No optimistic
updates.

**Error handling:**
Toast notification on failure with the problem detail message. Version conflicts prompt to retry (refresh first for
rollback/restart).

### Command Palette

**Action registry** — new module alongside existing search:

```typescript
interface PaletteAction {
    id: string;
    keywords: string[];            // fuzzy match tokens
    steps: PaletteStep[];          // sequential parameter prompts
    execute: (...args) => Promise<void>;
    destructive?: boolean;         // adds confirmation step
}

interface PaletteStep {
    type: "resource" | "number" | "text" | "choice";
    resourceType?: string;         // for resource picker
    label: string;
    choices?: { label: string; value: string }[];  // for choice type
}
```

**Registered actions:**

| Command     | Keywords           | Steps                             |
|-------------|--------------------|-----------------------------------|
| Scale       | scale, replicas    | service picker → number input     |
| Image       | image, deploy, tag | service picker → image text input |
| Rollback    | rollback, revert   | service picker → confirm          |
| Restart     | restart, redeploy  | service picker → confirm          |
| Drain       | drain              | node picker → confirm             |
| Activate    | activate, undrain  | node picker                       |
| Pause       | pause              | node picker → confirm             |
| Remove Task | remove, kill, task | task picker → confirm             |

**Palette UX flow:**

1. User opens Cmd+K, types "scale web"
2. Fuzzy match: "scale" matches the Scale action, "web" filters services
3. User selects service → palette advances to replicas input
4. User enters number, hits enter → execute
5. Palette closes, toast confirms

**Free-form shorthand:**

- If input matches `[action] [target] [value]` completely (e.g., "scale web-api 5"), show a single "Run: Scale web-api
  to 5 replicas" entry — enter to execute
- If incomplete, drop into guided step flow

**Destructive actions** always insert a confirmation step before executing.

## Implementation Phases

### Phase 1 — Vertical Slice: Scale Service

Establishes all patterns end-to-end:

1. `ScaleService` method on Docker client
2. `requireWrite` middleware
3. `PUT /services/{id}/scale` route + handler
4. Frontend `put`/`post`/`patch`/`del` helpers + `api.scaleService`
5. Scale popover on service detail page
6. Palette action registry + "scale" command
7. Tests for each layer

### Phase 2 — Remaining Tier 1 Operations

8. Update image — PUT (route + handler + popover + palette)
9. Rollback — POST (route + handler + button + palette)
10. Restart — POST (route + handler + button + palette)

### Phase 3 — Node Operations + Task Removal

11. Node availability — PUT (route + handler + dropdown + palette)
12. Force-remove task — DELETE (route + handler + button + palette)

### Phase 4 — Tier 2 Sub-Resource Endpoints

13. Service env — GET + PATCH with RFC 6902 JSON Patch
14. Node labels — GET + PATCH with RFC 6902 JSON Patch
15. Service resources — GET + PATCH with RFC 7396 JSON Merge Patch

Each phase is independently shippable.

## Testing Strategy

**Backend:**

- Unit tests for each Docker client write method (mock Docker API)
- Handler tests: valid request → 200, invalid body → 400, not found → 404, version conflict → 409
- `requireWrite` middleware test: pass-through behavior (scaffolding for future RBAC)
- JSON Patch handler tests: add/remove/replace/test operations, unsupported ops → 400, test failure → 409, missing
  Content-Type → 415, paths with and without leading slash
- JSON Merge Patch handler tests: partial update, null deletion, wrong Content-Type → 415
- Integration: mutation → watcher event → cache update → SSE broadcast (existing test patterns)

**Frontend:**

- `put`/`post`/`patch`/`del` helpers: success/error/401 handling
- Action component tests: popover renders, submits correct payload, shows loading state
- Palette: action matching, step flow, free-form parsing
- E2E: trigger action → verify SSE update renders

## OpenAPI

All new endpoints will be added to `api/openapi.yaml` with request/response schemas, including the
`application/json-patch+json` and `application/merge-patch+json` content types for PATCH endpoints.
