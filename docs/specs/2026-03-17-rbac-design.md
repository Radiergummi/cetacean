# RBAC Authorization Design

**Date:** 2026-03-17
**Status:** Draft

## Overview

Add authorization to Cetacean via a grant-based RBAC system. Grants are tuples of `(resources, audience, permissions)` with glob wildcard support. The system is additive-only (no deny rules), provider-pluggable for grant sources, and enforced at both the API and UI level.

## Grant Model

A grant consists of three fields:

```yaml
grants:
  - resources: ["stack:webapp-*", "stack:api-*"]
    audience: ["group:engineering"]
    permissions: ["read", "write"]

  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["read", "write"]

  - resources: ["stack:public-*"]
    audience: ["user:*@example.com"]
    permissions: ["read"]
```

### Resource Expressions

Format: `type:pattern`

- `type` is singular: `service`, `stack`, `node`, `task`, `config`, `secret`, `network`, `volume`
- `pattern` supports glob wildcards (`*`, `?`)
- Bare `*` is shorthand for `*:*` (all resources)
- Examples: `stack:webapp`, `service:*`, `node:orb-*`, `config:traefik-*`

### Audience Expressions

Format: `kind:pattern`

- `user:pattern` matches against both `Identity.Subject` and `Identity.Email` (union). This handles providers where `Subject` is an opaque ID (e.g., OIDC `sub` claim) while `Email` is the human-readable identifier.
- `group:pattern` matches each entry in `Identity.Groups`
- Bare `*` is shorthand for `*:*` (everyone)
- Examples: `user:alice@example.com`, `user:*@example.com`, `group:eng-*`, `group:ops`

### Permissions

- `read` — view the resource in lists, detail pages, SSE streams, and search
- `write` — mutate the resource (future write operations)
- `write` implies `read`
- Additive: the union of all matching grants determines effective permissions

## Policy Engine (`internal/acl`)

### Core Types

```go
type Grant struct {
    Resources   []string `json:"resources" yaml:"resources" toml:"resources"`
    Audience    []string `json:"audience" yaml:"audience" toml:"audience"`
    Permissions []string `json:"permissions" yaml:"permissions" toml:"permissions"`
}

type Policy struct {
    Grants []Grant `json:"grants" yaml:"grants" toml:"grants"`
}
```

### Evaluator

The `Evaluator` is the main entry point. It holds a file-based policy (atomically swappable for hot reload) and an optional provider-specific `GrantSource`.

```go
type Evaluator struct {
    policy   atomic.Pointer[Policy]
    source   GrantSource       // optional, provider-specific
    resolver ResourceResolver  // for stack and task resolution
}

// Can checks if the identity has the given permission on the resource.
// resource is "type:name", e.g. "service:webapp-api".
func (e *Evaluator) Can(id *auth.Identity, permission string, resource string) bool

// Filter returns only items the identity can access.
func Filter[T any](e *Evaluator, id *auth.Identity, permission string, items []T, resourceFunc func(T) string) []T
```

### Evaluation Logic

For a given `(identity, permission, resource)`:

1. Collect all file-based grants where the audience matches the identity
2. Collect all provider-sourced grants via `GrantSource.GrantsFor(id)` (if configured)
3. Union all matching grants
4. Check if any matched grant covers the requested resource and permission
5. `write` permission implies `read`

### Resource Resolution

The evaluator needs to resolve two relationships that cross resource boundaries: stack membership and task parentage. These are provided via a single interface:

```go
type ResourceResolver interface {
    // StackOf returns the stack name for a resource, or "" if it doesn't belong to one.
    // Implementation reads the com.docker.stack.namespace label directly from the resource.
    StackOf(resourceType, resourceID string) string

    // ServiceOfTask returns the service name for a task, or "" if unknown.
    // Used for task permission inheritance.
    ServiceOfTask(taskID string) string
}
```

The cache implements this interface. `StackOf` reads the `com.docker.stack.namespace` label from the resource (no reverse index needed). `ServiceOfTask` looks up the task and returns its parent service name.

### Stack Resolution

Grants targeting `stack:X` cover all resources belonging to that stack. Resolution is reverse-lookup: during `Can()`, if no direct resource grant matches, the evaluator calls `ResourceResolver.StackOf()` to determine the resource's stack, then checks whether any `stack:` grant matches.

A `stack:X` grant gives access to the stack itself and all its member resources. Stack detail responses still filter member resources through `acl.Filter` for consistency, but in practice a stack grant covers all members.

### Task Inheritance

Tasks inherit permissions from their parent service. When evaluating `Can(id, "read", "task:...")`, the evaluator calls `ResourceResolver.ServiceOfTask(taskID)` to resolve the parent service, then checks access against `service:<serviceName>`. Explicit `task:` grants also work but have limited utility since task IDs are opaque Docker IDs.

### Node Resource Names

Node grants use hostnames (e.g., `node:orb-*`), not Docker IDs. Handlers pass `node:<hostname>` (from `node.Description.Hostname`) to the evaluator, not the Docker node ID.

### Default Behavior

| Condition | Result |
|---|---|
| Auth mode `none` | No authorization, full access (current behavior) |
| Auth provider active, no policy configured | Full access for all authenticated users |
| Auth provider active, policy configured | Default-deny; only explicitly granted access |

## Grant Sources

Grants come from a file-based policy and optionally from the auth provider.

### File-Based Policy (All Providers)

- `CETACEAN_ACL_POLICY` — inline JSON, YAML, or TOML string
- `CETACEAN_ACL_POLICY_FILE` — path to a policy file
- Precedence: `CETACEAN_ACL_POLICY` > `CETACEAN_ACL_POLICY_FILE`
- Format detection: by file extension for `_FILE` (`.json`, `.yaml`/`.yml`, `.toml`); for inline `_POLICY` (no extension), try JSON → TOML → YAML. JSON is recommended for inline policies to avoid ambiguity.
- Validation at load time: reject invalid resource types, audience kinds, or permission values with clear error messages

### Provider-Specific Grant Sources

```go
type GrantSource interface {
    GrantsFor(id *auth.Identity) []Grant
}
```

Provider grants omit the `audience` field — they are implicitly scoped to the authenticated user.

| Provider | Source | Config |
|---|---|---|
| Tailscale | `CapMap` peer capability | `CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY` |
| OIDC | Custom token claim | `CETACEAN_AUTH_OIDC_ACL_CLAIM` |
| Headers | Proxy-injected header (JSON) | `CETACEAN_AUTH_HEADERS_ACL` |
| Cert | None (identity only, use file policy) | — |
| None | N/A (no authorization) | — |

### Grant Composition

Provider grants and file grants are a pure union. There is no intersection or ceiling — if either source grants access, the user has it. This matches the trust model: if you trust the provider for identity, you trust it for grants.

## Hot Reload

- File watching via `fsnotify` on the `CETACEAN_ACL_POLICY_FILE` path
- On change: re-read, re-parse, re-validate
- Valid policy: atomic swap in the evaluator, log the change
- Invalid policy: log the error, keep previous policy
- Debounce file events to handle editor multi-write patterns
- Inline env var (`CETACEAN_ACL_POLICY`) is not hot-reloadable (requires restart, consistent with all other env vars)
- Provider grant sources are inherently per-request (always fresh)

## API Integration

### List Handlers

One `Filter` call after cache retrieval, before pagination:

```go
services := h.cache.ListServices()
services = acl.Filter(h.acl, id, "read", services, func(s swarm.Service) string {
    return "service:" + s.Spec.Name
})
// existing search, filter, sort, paginate...
```

### Detail Handlers

One `Can` check before returning the resource:

```go
if !h.acl.Can(id, "read", "service:"+svc.Spec.Name) {
    writeProblem(w, r, 403, "forbidden", "access denied")
    return
}
```

Cross-reference lists ("services using this config") are filtered to only include services the user can read.

### Global Search

Results filtered per resource type before returning. Each type's results pass through `Filter`.

### SSE Streams

The `cache.Event` struct gains a `Name` field (populated by the cache's `notify` method when firing events) so that SSE authorization can check by resource name without type-asserting `Resource` on every event.

The broadcaster's per-client match function is wrapped with an authorization check:

```go
match := func(ev Event) bool {
    if !typeMatcher(ev) { return false }
    return e.Can(id, "read", ev.Type+":"+ev.Name)
}
```

On policy hot-reload, existing SSE connections continue with the previous policy until reconnect. This is an acceptable trade-off vs. the complexity of force-disconnecting clients.

### Cluster-Wide Endpoints

Endpoints that don't map to a specific resource (`/cluster`, `/cluster/metrics`, `/swarm`, `/disk-usage`, `/plugins`, `/stacks/summary`, `/history`, `/topology/*`) are accessible to any authenticated user who has at least one grant. The rationale: if you can see anything in the cluster, you can see aggregate cluster information. These endpoints don't expose individual resource details that would bypass per-resource filtering.

### `/auth/whoami` Extension

The whoami response gains a `permissions` field summarizing effective access:

```json
{
  "subject": "alice@example.com",
  "displayName": "Alice",
  "provider": "oidc",
  "permissions": {
    "service:webapp-*": ["read", "write"],
    "stack:monitoring": ["read"]
  }
}
```

This is a projection of the raw grant patterns matching the user (not resolved to actual resources). The frontend uses it to adapt the UI without needing the full policy.

## Frontend Integration

### Auth Hook

`useAuth` exposes the `permissions` map from the whoami response.

### Access Check Hook

```typescript
function useCanAccess(resource: string, permission: string): boolean
```

Simple `*`-only glob matching against the permissions map (no `?` support needed client-side — keep it minimal). Used to conditionally render UI elements.

### Behavior

- **List pages:** No change needed — the API returns only visible resources
- **Detail pages:** If a user navigates to a forbidden resource, the API returns 403, handled by existing `FetchError` component
- **Navigation/sidebar:** Stacks and resources the user can't read are hidden
- **Write UI (future):** Buttons/actions hidden when user lacks `write` permission
- **Search:** Results already filtered server-side

## Security Considerations

- **Default-deny with policy:** Once a policy is loaded, users without matching grants see nothing
- **Default-allow without policy:** No policy means full access for authenticated users (preserves backward compatibility)
- **Auth mode `none` bypasses ACL:** Intentional for trusted environments
- **Secret data unchanged:** `sec.Spec.Data = nil` behavior is independent of ACL. Having `read` on a secret shows metadata, not contents (which the API never exposes)
- **Policy file permissions:** Log a warning at startup if the policy file is world-readable
- **Provider trust:** Provider-sourced grants are as trustworthy as the identity itself. If you trust the IdP for identity, you trust it for grants
- **No deny rules:** Absence of a grant is the only denial. Avoids the confusion and debugging difficulty of allow+deny interactions

## Configuration Reference

| Variable | Default | Description |
|---|---|---|
| `CETACEAN_ACL_POLICY` | — | Inline policy (JSON, YAML, or TOML) |
| `CETACEAN_ACL_POLICY_FILE` | — | Path to policy file (watched for changes) |
| `CETACEAN_AUTH_TAILSCALE_ACL_CAPABILITY` | — | Tailscale capability key for per-user grants |
| `CETACEAN_AUTH_OIDC_ACL_CLAIM` | — | OIDC token claim containing grants |
| `CETACEAN_AUTH_HEADERS_ACL` | — | HTTP header containing grants (JSON) |
