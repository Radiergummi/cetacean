# SSE-Derived Sub-Resource State

## Summary

Refactor ServiceDetail and NodeDetail to derive sub-resource state from SSE event payloads instead of refetching everything on every event. Reduces ServiceDetail from 10 HTTP requests per SSE event to 2, and NodeDetail from 5 to 2.

## Problem

ServiceDetail's `fetchData` makes 10 parallel HTTP requests on every SSE event. NodeDetail makes 5. The SSE event already delivers the full resource object (the complete `swarm.Service` or `swarm.Node` struct), but this payload is ignored. Of those requests, only `tasks` and `history` return data not present in the resource object — the other 8 (ServiceDetail) or 3 (NodeDetail) are redundant.

## Decisions

- **Scope**: ServiceDetail + NodeDetail only. `useDetailResource` (used by simpler detail pages) is out of scope.
- **Post-mutation updates**: Editors keep `onSaved` callbacks for immediate feedback. SSE reconciles moments later.
- **Fallback**: Sync events (no `resource` payload) trigger full refetch as before.

## Design

### ServiceDetail

**Split `fetchData` into two concerns:**

1. `fetchService(signal)` — calls `api.service(id)`, sets `service`/`changes`, and derives all sub-resource state from the response.
2. `fetchSideData(signal)` — calls `api.serviceTasks(id)` and `api.history(...)`. These are the only two data sources not derivable from the service object.

**New `deriveSubResources(service: Service)` helper** that extracts and sets state for all sub-resources:

| State variable | Derivation path | Transformation |
|---|---|---|
| `envVars` | `ContainerSpec.Env` | Split `string[]` (`KEY=VALUE`) into `Record<string, string>` |
| `serviceResources` | `TaskTemplate.Resources` | None (direct assignment) |
| `serviceLabels` | `Spec.Labels` | Nil coercion to `{}` |
| `healthcheck` | `ContainerSpec.Healthcheck` | None |
| `specPorts` | `EndpointSpec.Ports` | Nil coercion to `[]` |
| `serviceMounts` | `ContainerSpec.Mounts` | Nil coercion to `[]` |
| `containerConfig` | `ContainerSpec.*` | Flatten command/args/user/dir/hostname/groups/stopGracePeriod/stopSignal/readOnly/init/tty/dns/capabilities/extraHosts |
| `serviceConfigs` | `ContainerSpec.Configs` | Project `ConfigReference[]` → `ServiceConfigRef[]` (extract configID, configName, fileName) |
| `serviceSecrets` | `ContainerSpec.Secrets` | Project `SecretReference[]` → `ServiceSecretRef[]` (extract secretID, secretName, fileName) |
| `serviceNetworks` | `TaskTemplate.Networks` | Project `NetworkAttachmentConfig[]` → `ServiceNetworkRef[]` (extract target, aliases) |

The env split and config/secret/network projections replicate the same logic currently in the backend handlers (`envSliceToMap`, `extractConfigRefs`, `extractSecretRefs`, `extractNetworkRefs`). These are trivial transformations (3-5 lines each).

**SSE listener** changes from:
```ts
useResourceStream(`/services/${id}`, fetchData);
```
To a function that:
1. Checks if `event.resource` is present.
2. If present: cast to `Service`, call `setService(service)`, `deriveSubResources(service)`, and call `fetchSideData(signal)` for tasks + history only. Note: `changes` (spec diff) is NOT updated on this path — it requires server-side `DiffServiceSpecs` which is not available in the SSE payload. `changes` updates only on sync events or page load. This is acceptable because spec diffs are informational and the Last Deployment section is not time-critical.
3. If absent (sync events): call both `fetchService(signal)` and `fetchSideData(signal)` as fallback. This updates `changes` too.

**On mount**: `fetchService` + `fetchSideData` run in parallel. Sub-resources derived from the initial service fetch — no separate sub-resource HTTP calls needed.

**`onSaved` callbacks remain unchanged.** Editors update state immediately from PATCH responses. The next SSE event (arriving within ~100ms) reconciles by re-deriving all sub-resources from the updated service.

**Net effect:**
- Initial page load: 4 HTTP requests (service + tasks + history + networks list) instead of 11+
- SSE event with resource: 2 HTTP requests (tasks + history) instead of 10
- SSE sync event (no resource): 4 HTTP requests (full refetch fallback)

Note: the networks list fetch (`api.networks({ limit: 0 })` for display name resolution) fires once on mount in a separate `useEffect` and is not part of the SSE-triggered refetch path. It is counted in the initial load total.

### NodeDetail

Same refactoring pattern.

**Split `fetchData`:**
1. `fetchNode(signal)` — calls `api.node(id)`, sets `node`, derives `nodeLabels` from `node.Spec.Labels`.
2. `fetchSideData(signal)` — calls `api.nodeTasks(id)`, `api.history(...)`, and `api.nodeRole(id)`.

`nodeRole` stays as a fetch because `managerCount` requires iterating all nodes — it's not derivable from the single node object.

**SSE listener**: same pattern — use `event.resource` for optimistic node update + derive labels, refetch tasks/history/role.

**Net effect:**
- Initial page load: 4 HTTP requests instead of 5
- SSE event with resource: 3 HTTP requests instead of 5

### Frontend Type Changes

All `ContainerSpec` fields needed for sub-resource derivation (Env, Command, Args, User, Dir, Hostname, StopGracePeriod, StopSignal, ReadOnly, Init, TTY, Groups, DNSConfig, CapabilityAdd, CapabilityDrop, Hosts, Configs, Secrets) are already present in the TypeScript `Service` type in `frontend/src/api/types.ts`. No type additions are needed.

### What Stays the Same

- All sub-resource GET/PATCH backend endpoints remain unchanged
- Backend SSE broadcast remains unchanged
- `useResourceStream` hook remains unchanged
- Editor components remain unchanged (receive data via props, call `onSaved`)
- List pages (`useSwarmResource`) remain unchanged
- Other detail pages (`useDetailResource`) remain unchanged
