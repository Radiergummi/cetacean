# Allow Header UI Gating

Replace the global operations-level context in the frontend with per-resource permission
gating driven by the `Allow` response header. The backend already computes the correct
`Allow` header on every response, combining operations level and ACL write permission.
The frontend currently ignores it and uses a separate global operations-level number
fetched from `/-/health`. This design unifies both into a single mechanism.

## Principle

**Default-deny in the UI.** Write controls are hidden until the backend explicitly
permits them via the `Allow` header. No separate permission fetch, no client-side
tier arithmetic — the backend is the single source of truth.

## API Client

`fetchJSON<T>` changes its return type:

```ts
// Before
async function fetchJSON<T>(path: string, signal?: AbortSignal): Promise<T>

// After
interface FetchResult<T> {
  data: T;
  allowedMethods: Set<string>;
}

async function fetchJSON<T>(path: string, signal?: AbortSignal): Promise<FetchResult<T>>
```

The `Allow` response header is parsed into a `Set<string>`. If the header is absent,
the set is empty (default-deny). All existing callers are updated — most destructure
`{ data }` and ignore `allowedMethods`.

`mutationFetch` is unchanged (mutations don't need the `Allow` header).

## `useDetailResource` Hook

Currently returns `{ data, history, error, retry }`. Adds `allowedMethods: Set<string>`
(default: empty set). Populated from the `fetchJSON` return value. Updated on every
re-fetch (including SSE-triggered re-fetches), so a policy hot-reload is reflected
in the UI on the next event.

## Component Changes

Every component that currently reads `useOperationsLevel()` switches to receiving
`allowedMethods` (or a derived `canEdit` / `canDelete` boolean) from its parent
detail page. The mapping from tier checks to method checks:

| Current check | New check |
|---|---|
| `level >= opsLevel.operational` | `allowedMethods.has("PUT")` or `.has("POST")` |
| `level >= opsLevel.configuration` | `allowedMethods.has("PATCH")` |
| `level >= opsLevel.impactful` | `allowedMethods.has("DELETE")` |

### `EditablePanel`

Currently reads `useOperationsLevel()` internally and computes `canEdit` from the
level. Changes to accept a `canEdit: boolean` prop from its parent instead.
This removes the hidden context dependency and makes the component a pure
display/edit toggle.

### `ServiceActions`, `NodeActions`, `StackActions`, `SwarmActions`

Currently gate buttons on ops level. Switch to receiving `allowedMethods` as a prop.
Rollback/restart are gated on `POST`, scale/image on `PUT`, delete on `DELETE`.

### `ReplicaCard`, `ContainerImage`

Currently gate inline scale and image-update controls on ops level. Switch to
receiving `allowedMethods` or a `canWrite` boolean from the parent.

### `AvailabilityEditor`, `RoleEditor`

Node-specific editors. Currently read ops level internally. Switch to `canEdit` prop.

### `RemoveResourceAction`

Generic delete button used on config, secret, network, volume, stack detail pages.
Currently reads ops level. Switch to receiving `canDelete: boolean` from parent.

### Editor components (`EnvEditor`, `PortsEditor`, `ResourcesEditor`, etc.)

Most of these are wrapped by `EditablePanel` and don't check ops level themselves.
Those that do (`PortsEditor`, `ConfigsEditor`, `SecretsEditor`, `NetworksEditor`,
`MountsEditor`, `HealthcheckEditor`, `EndpointModeEditor`, `ResourcesEditor`) are
for cases where the component is rendered outside `EditablePanel` or needs the level
for conditional logic. All switch to a `canEdit` prop.

## Deletions

- `OperationsLevelProvider` component
- `OperationsLevelContext`
- `useOperationsLevel` hook
- `opsLevel` constants
- The `/-/health` fetch in the provider (the health endpoint itself stays; only
  the frontend consumer is removed)

## Special Cases

### SearchPalette

The command palette shows write actions (scale, restart, rollback, etc.) before a
resource is selected. At that point we don't know per-resource permissions, so the
full action list is shown.

Once the user selects a target resource, a `HEAD` request is fired to the resource's
detail path. The `Allow` header from the response gates whether the action proceeds.
If the method is not permitted, the action is disabled with a message. `HEAD` is
cheap — no body, same middleware chain, backend already sets `Allow` on `HEAD`.

### List Endpoints — `POST` in `Allow`

The backend's `setAllowList` currently hardcodes `Allow: GET, HEAD`. This is extended
to include `POST` on resource types that support creation (configs, secrets, plugins)
when the user's ops level and ACL permit it. The pattern mirrors `setAllow` for detail
endpoints.

This lets `CreateResourceDialog` (config/secret creation on list pages) and the plugin
install button on the plugins list page read `allowedMethods` from their list fetch
response.

### `useSwarmResource` (List Hook)

Currently returns `{ items, total, ... }`. Updated to also expose `allowedMethods`
from the list fetch, so list pages can pass it to `CreateResourceDialog` and similar
components.

## Backend Changes

### `setAllowList`

Extended to accept a resource type and check ops level + ACL for `POST`:

```go
func (h *Handlers) setAllowList(w http.ResponseWriter, r *http.Request, resourceType string) {
    methods := []string{"GET", "HEAD"}
    // Check if POST is available for this resource type at the current
    // ops level and ACL.
    // ...
    w.Header().Set("Allow", strings.Join(methods, ", "))
}
```

Resource types with `POST` support: `config`, `secret`, `plugin`.

For `POST` on list endpoints, the ACL check uses `Can(id, "write", "config:*")`
(and similarly for secrets and plugins). This is a heuristic: it matches grants
with broad patterns (`config:*`, `*`) but not narrow ones like `config:traefik-routes`.
This is acceptable — a user with only a narrow grant can still create via the API,
but won't see the button. False negatives are preferable to false positives for
a create action.

## Not in Scope

- Changing the `Allow` header semantics on the backend (already correct).
- Adding new API endpoints.
- Changing ACL evaluation logic.
- Modifying SSE event payloads.
