# Write Operations for Cetacean

**Date:** 2026-03-17
**Status:** Approved

## Goal

Add careful write operations to Cetacean's read-only Docker Swarm dashboard. The scope covers operational triage (draining nodes, scaling services, removing stuck tasks) and service tuning (updating images, rollback, restart, env vars, resource limits). Deploying new services or stacks is explicitly out of scope.

## Constraints

- No service/stack creation — only mutations to existing resources
- Authorization is deferred: authenticated = authorized for now, with the architecture ready for role-based authorization later
- All mutations go through the Docker API; the existing watcher/cache/SSE pipeline handles propagation to all clients
- No optimistic UI updates — wait for SSE confirmation

## Operations

### Tier 1 — High-frequency operational actions

| Operation | Route | Request Body | Response |
|-----------|-------|-------------|----------|
| Scale service | `POST /services/{id}/scale` | `{"replicas": 5}` | 200 + updated service |
| Update service image | `POST /services/{id}/image` | `{"image": "nginx:1.27"}` | 200 + updated service |
| Rollback service | `POST /services/{id}/rollback` | (empty) | 200 + updated service |
| Restart service | `POST /services/{id}/restart` | (empty) | 200 + updated service |
| Set node availability | `POST /nodes/{id}/availability` | `{"availability": "drain"}` | 200 + updated node |
| Force-remove task | `DELETE /tasks/{id}` | (none) | 204 |

**Note on task removal:** Docker Swarm has no direct task removal API. "Force-remove task" is implemented by inspecting the task's `Status.ContainerStatus.ContainerID` and calling `ContainerRemove` with `Force: true`. This kills the backing container; the swarm scheduler then reconciles (rescheduling if the service still demands replicas). If the task has no container (e.g., already exited), the endpoint returns 404.

### Tier 2 — Less frequent service tuning

| Operation | Route | Request Body | Response |
|-----------|-------|-------------|----------|
| Update node labels | `POST /nodes/{id}/labels` | `{"set": {...}, "remove": [...]}` | 200 + updated node |
| Update service env | `POST /services/{id}/env` | `{"set": {"K": "V"}, "remove": ["K"]}` | 200 + updated service |
| Update service resources | `POST /services/{id}/resources` | `{"limits": {...}, "reservations": {...}}` | 200 + updated service |

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
    svc, _, err := c.cl.ServiceInspectWithRaw(ctx, id, types.ServiceInspectOptions{})
    if err != nil {
        return swarm.Service{}, err
    }
    if svc.Spec.Mode.Replicated == nil {
        return swarm.Service{}, fmt.Errorf("cannot scale a global-mode service")
    }
    svc.Spec.Mode.Replicated.Replicas = &replicas
    resp, err := c.cl.ServiceUpdate(ctx, id, svc.Version, svc.Spec, types.ServiceUpdateOptions{})
    if err != nil {
        return swarm.Service{}, err
    }
    // resp.Warnings can be logged
    // Re-inspect to get authoritative server state
    svc, _, err = c.cl.ServiceInspectWithRaw(ctx, id, types.ServiceInspectOptions{})
    return svc, err
}
```

For task removal, the method inspects the task's `Status.ContainerStatus.ContainerID` and calls `ContainerRemove` with `Force: true`.

**Validation rules:**
- `ScaleService`: reject if service is global-mode (return error, handler maps to 400)
- `UpdateServiceImage`: reject empty image string
- `UpdateNodeAvailability`: reject values other than "active", "drain", "pause"

For rollback, use `ServiceUpdate` with `Rollback: "previous"` in the update options.
For restart (force update), increment `ForceUpdate` on the task template without changing the spec.

### Routes

Action-oriented `POST` sub-resources on `internal/api/router.go`:

```go
mux.Handle("POST /services/{id}/scale", requireWrite(h.HandleScaleService))
mux.Handle("POST /services/{id}/image", requireWrite(h.HandleUpdateServiceImage))
mux.Handle("POST /services/{id}/rollback", requireWrite(h.HandleRollbackService))
mux.Handle("POST /services/{id}/restart", requireWrite(h.HandleRestartService))
mux.Handle("POST /services/{id}/env", requireWrite(h.HandleUpdateServiceEnv))
mux.Handle("POST /services/{id}/resources", requireWrite(h.HandleUpdateServiceResources))
mux.Handle("POST /nodes/{id}/availability", requireWrite(h.HandleUpdateNodeAvailability))
mux.Handle("POST /nodes/{id}/labels", requireWrite(h.HandleUpdateNodeLabels))
mux.Handle("DELETE /tasks/{id}", requireWrite(h.HandleRemoveTask))
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

This is wired in `main.go` alongside the existing `DockerLogStreamer` and `DockerSystemClient`. The interface keeps handlers testable via mocks.

**Error detection:** Use `errdefs.IsConflict(err)` and `errdefs.IsNotFound(err)` from `github.com/docker/docker/errdefs` to translate Docker errors to appropriate HTTP status codes (409, 404). All other Docker errors map to 500.

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

When auth mode is `none`, the `NoneProvider` injects an anonymous identity upstream, so writes are allowed in trusted/single-user environments. The auth exemption paths (`/-/*`, `/api*`, `/assets/*`, `/auth/*`) do NOT cover write routes — all write endpoints go through the auth middleware.

### Response Patterns

- **Success**: 200 with JSON-LD wrapped updated resource (same format as detail GET endpoints), or 204 for deletions
- **Version conflict**: 409 with RFC 9457 problem detail — "resource was modified by another client, please retry"
- **Not found**: 404 with problem detail
- **Validation error**: 400 with problem detail (e.g., replicas < 0, empty image string)
- **In use**: 409 with problem detail (for future delete operations — "secret is used by services X, Y")

**Content negotiation:** Write routes bypass the `contentNegotiated`/`contentNegotiatedWithSSE` dispatch wrappers. The `negotiate` middleware still runs (it's in the global chain) but write handlers always respond with JSON regardless of the negotiated content type. This is intentional — write endpoints are API-only, never served as HTML.

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

This means the mutating client sees the update via SSE just like every other client. No special-case cache invalidation needed.

## Frontend Design

### Mutation Client

Two new helpers in `frontend/src/api/client.ts`:

```typescript
async function post<T>(path: string, body?: unknown): Promise<T> {
    const res = await fetch(path, {
        method: "POST",
        headers: {
            "Content-Type": "application/json",
            "Accept": "application/json",
            ...authHeaders(),
        },
        body: body !== undefined ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) {
        const problem = await res.json();
        throw new Error(problem.detail || res.statusText);
    }
    return res.json();
}

async function del(path: string): Promise<void> {
    const res = await fetch(path, {
        method: "DELETE",
        headers: { "Accept": "application/json", ...authHeaders() },
    });
    if (!res.ok) {
        const problem = await res.json();
        throw new Error(problem.detail || res.statusText);
    }
}
```

Per-operation typed functions:

```typescript
api.scaleService(id: string, replicas: number)        // post(`/services/${id}/scale`, { replicas })
api.updateServiceImage(id: string, image: string)      // post(`/services/${id}/image`, { image })
api.rollbackService(id: string)                        // post(`/services/${id}/rollback`)
api.restartService(id: string)                         // post(`/services/${id}/restart`)
api.updateNodeAvailability(id: string, availability: "active" | "drain" | "pause")  // post(...)
api.removeTask(id: string)                             // del(`/tasks/${id}`)
// Tier 2:
api.updateNodeLabels(id: string, set: Record<string, string>, remove: string[])
api.updateServiceEnv(id: string, set: Record<string, string>, remove: string[])
api.updateServiceResources(id: string, limits: ResourceSpec, reservations: ResourceSpec)
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
Action buttons show a spinner between submission and the SSE event arriving with the updated resource. No optimistic updates.

**Error handling:**
Toast notification on failure with the problem detail message. Version conflicts prompt to retry.

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

| Command | Keywords | Steps |
|---------|----------|-------|
| Scale | scale, replicas | service picker → number input |
| Image | image, deploy, tag | service picker → image text input |
| Rollback | rollback, revert | service picker → confirm |
| Restart | restart, redeploy | service picker → confirm |
| Drain | drain | node picker → confirm |
| Activate | activate, undrain | node picker |
| Pause | pause | node picker → confirm |
| Remove Task | remove, kill, task | task picker → confirm |

**Palette UX flow:**

1. User opens Cmd+K, types "scale web"
2. Fuzzy match: "scale" matches the Scale action, "web" filters services
3. User selects service → palette advances to replicas input
4. User enters number, hits enter → execute
5. Palette closes, toast confirms

**Free-form shorthand:**
- If input matches `[action] [target] [value]` completely (e.g., "scale web-api 5"), show a single "Run: Scale web-api to 5 replicas" entry — enter to execute
- If incomplete, drop into guided step flow

**Destructive actions** always insert a confirmation step before executing.

## Implementation Phases

### Phase 1 — Vertical Slice: Scale Service
Establishes all patterns end-to-end:
1. `ScaleService` method on Docker client
2. `requireWrite` middleware
3. `POST /services/{id}/scale` route + handler
4. Frontend `mutate` helper + `api.scaleService`
5. Scale popover on service detail page
6. Palette action registry + "scale" command
7. Tests for each layer

### Phase 2 — Remaining Service Operations
8. Update image (route + handler + popover + palette)
9. Rollback (route + handler + button + palette)
10. Restart / force update (route + handler + button + palette)

### Phase 3 — Node Operations
11. Node availability: drain/activate/pause (route + handler + dropdown + palette)
12. Node labels (route + handler + edit UI + palette)

### Phase 4 — Task Removal
13. Force-remove task (route + handler + button + palette)

### Phase 5 — Tier 2 Service Tuning
14. Environment variables (route + handler + edit UI + palette)
15. Resource limits (route + handler + edit UI + palette)

Each phase is independently shippable.

## Testing Strategy

**Backend:**
- Unit tests for each Docker client write method (mock Docker API)
- Handler tests: valid request → 200, invalid body → 400, not found → 404, version conflict → 409
- `requireWrite` middleware test: unauthenticated → 401, authenticated → passes through
- Integration: mutation → watcher event → cache update → SSE broadcast (existing test patterns)

**Frontend:**
- `mutate` helper: success/error/401 handling
- Action component tests: popover renders, submits correct payload, shows loading state
- Palette: action matching, step flow, free-form parsing
- E2E: trigger action → verify SSE update renders

## OpenAPI

All new endpoints will be added to `api/openapi.yaml` with request/response schemas.
