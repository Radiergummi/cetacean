# Service Config Write Operations Design

## Overview

Add write endpoints for five service configuration sub-resources: placement, published ports, update policy, rollback policy, and log driver. All are tier 1 (operational) and backend-only (no frontend editors in this scope).

## Endpoints

| Resource        | GET                                  | Write                                               | Tier |
|-----------------|--------------------------------------|-----------------------------------------------------|------|
| Placement       | `GET /services/{id}/placement`       | `PUT /services/{id}/placement`                      | 1    |
| Published ports | `GET /services/{id}/ports`           | `PATCH /services/{id}/ports` (merge patch)          | 1    |
| Update policy   | `GET /services/{id}/update-policy`   | `PATCH /services/{id}/update-policy` (merge patch)  | 1    |
| Rollback policy | `GET /services/{id}/rollback-policy` | `PATCH /services/{id}/rollback-policy` (merge patch) | 1   |
| Log driver      | `GET /services/{id}/log-driver`      | `PATCH /services/{id}/log-driver` (merge patch)     | 1    |

All GET endpoints return JSON-LD wrapped responses with ETag support and serve the SPA for HTML via `contentNegotiated`. All write endpoints go through `tier1` (`config.OpsOperational`) middleware.

## Tier Justification

These are tier 1 (operational) because they are service-level configuration changes — the same category as env, labels, resources, and healthcheck patches. They modify how a service runs but don't affect cluster topology (tier 2) or create/delete resources (future tier 3). While placement constraints and port changes can have side effects (rescheduling, connectivity), so can existing tier 1 operations like scaling to 0 or changing the image.

## Response Shapes

### Placement

`@type`: `ServicePlacement`

```json
{
  "@context": "...",
  "@id": "/services/{id}/placement",
  "@type": "ServicePlacement",
  "placement": {
    "Constraints": ["node.role==manager"],
    "Preferences": [{"Spread": {"SpreadDescriptor": "engine.labels.az"}}],
    "MaxReplicas": 0,
    "Platforms": []
  }
}
```

PUT replaces the entire placement object. The request body is the placement object itself (not wrapped). Sending an empty body `{}` sets placement to a zero-value `swarm.Placement{}` (no constraints, no preferences). To fully clear placement to nil, send `null`.

The GET handler reads from `svc.Spec.TaskTemplate.Placement`. If nil, returns an empty `swarm.Placement{}`.

### Published Ports

`@type`: `ServicePorts`

```json
{
  "@context": "...",
  "@id": "/services/{id}/ports",
  "@type": "ServicePorts",
  "ports": [
    {"Protocol": "tcp", "TargetPort": 80, "PublishedPort": 8080, "PublishMode": "ingress"}
  ]
}
```

Since ports are an array, merge patch replaces the whole array (RFC 7396 semantics). This is effectively a full replace but uses the consistent merge-patch Content-Type.

The GET handler reads from `svc.Spec.EndpointSpec.Ports`. If `EndpointSpec` is nil, returns an empty slice.

**Implementation note**: The `UpdateServicePorts` client method must guard against nil `EndpointSpec` — initialize it if nil and preserve the existing `Mode` field when only updating ports.

### Update Policy

`@type`: `ServiceUpdatePolicy`

```json
{
  "@context": "...",
  "@id": "/services/{id}/update-policy",
  "@type": "ServiceUpdatePolicy",
  "updatePolicy": {
    "Parallelism": 1,
    "Delay": 5000000000,
    "FailureAction": "pause",
    "Monitor": 5000000000,
    "MaxFailureRatio": 0,
    "Order": "stop-first"
  }
}
```

PATCH body is a partial object — only include fields to change:

```json
{"Parallelism": 2, "Order": "start-first"}
```

The GET handler reads from `svc.Spec.UpdateConfig`. If nil, returns an empty `swarm.UpdateConfig{}`.

**Spec field**: `svc.Spec.UpdateConfig` (type `*swarm.UpdateConfig`)

### Rollback Policy

`@type`: `ServiceRollbackPolicy`

Same structure as update policy, at `@id` `/services/{id}/rollback-policy`, with key `rollbackPolicy`.

**Spec field**: `svc.Spec.RollbackConfig` (type `*swarm.UpdateConfig` — same type as update policy)

### Log Driver

`@type`: `ServiceLogDriver`

```json
{
  "@context": "...",
  "@id": "/services/{id}/log-driver",
  "@type": "ServiceLogDriver",
  "logDriver": {
    "Name": "json-file",
    "Options": {"max-size": "10m", "max-file": "3"}
  }
}
```

PATCH body is a partial object:

```json
{"Options": {"max-size": "20m"}}
```

Note: `Options` is a `map[string]string` which the `mergePatch` function treats as a nested object — keys present in the patch overwrite, keys absent are **preserved** (standard RFC 7396 recursive merge). To remove a key, set it to `null`. To replace the entire options map, include all desired keys.

The GET handler reads from `svc.Spec.TaskTemplate.LogDriver`. If nil, returns `null` for the `logDriver` field.

**Spec field**: `svc.Spec.TaskTemplate.LogDriver` (type `*swarm.Driver`)

## Backend Pattern

Each endpoint follows the existing resources/healthcheck template.

### Docker client methods

```
Inspect service → mutate spec field → ServiceUpdate (with version) → re-inspect
```

New methods on `Client`:
- `UpdateServicePlacement(ctx, id, *swarm.Placement) (swarm.Service, error)` — sets `svc.Spec.TaskTemplate.Placement`
- `UpdateServicePorts(ctx, id, []swarm.PortConfig) (swarm.Service, error)` — sets `svc.Spec.EndpointSpec.Ports`, guards nil `EndpointSpec`, preserves `Mode`
- `UpdateServiceUpdatePolicy(ctx, id, *swarm.UpdateConfig) (swarm.Service, error)` — sets `svc.Spec.UpdateConfig`
- `UpdateServiceRollbackPolicy(ctx, id, *swarm.UpdateConfig) (swarm.Service, error)` — sets `svc.Spec.RollbackConfig`
- `UpdateServiceLogDriver(ctx, id, *swarm.Driver) (swarm.Service, error)` — sets `svc.Spec.TaskTemplate.LogDriver`

### GET handler

Read from cache, extract sub-resource, wrap in `NewDetailResponse`, serve with `writeJSONWithETag`.

### Write handler

All write handlers must:
1. Limit body size with `http.MaxBytesReader(w, r.Body, 1<<20)` (1MB)
2. Log the action via `slog.Info("updating service <sub-resource>", "service", id)`
3. Return the updated sub-resource wrapped in `NewDetailResponse` (consistent with resources/healthcheck pattern)

Specifics:
- **PUT** (placement): decode body as `swarm.Placement`, call client method, return updated placement in `NewDetailResponse`
- **PATCH** (all others): validate `Content-Type: application/merge-patch+json` (return 415 if wrong), marshal current → `map[string]any`, unmarshal patch → `map[string]any`, apply `mergePatch()`, unmarshal back to typed struct, call client method, return updated sub-resource in `NewDetailResponse`

### Router registration

```go
// Placement
mux.HandleFunc("GET /services/{id}/placement", contentNegotiated(h.HandleGetServicePlacement, spa))
mux.Handle("PUT /services/{id}/placement", tier1(h.HandlePutServicePlacement))

// Ports
mux.HandleFunc("GET /services/{id}/ports", contentNegotiated(h.HandleGetServicePorts, spa))
mux.Handle("PATCH /services/{id}/ports", tier1(h.HandlePatchServicePorts))

// Update policy
mux.HandleFunc("GET /services/{id}/update-policy", contentNegotiated(h.HandleGetServiceUpdatePolicy, spa))
mux.Handle("PATCH /services/{id}/update-policy", tier1(h.HandlePatchServiceUpdatePolicy))

// Rollback policy
mux.HandleFunc("GET /services/{id}/rollback-policy", contentNegotiated(h.HandleGetServiceRollbackPolicy, spa))
mux.Handle("PATCH /services/{id}/rollback-policy", tier1(h.HandlePatchServiceRollbackPolicy))

// Log driver
mux.HandleFunc("GET /services/{id}/log-driver", contentNegotiated(h.HandleGetServiceLogDriver, spa))
mux.Handle("PATCH /services/{id}/log-driver", tier1(h.HandlePatchServiceLogDriver))
```

### DockerWriteClient interface

Add the five new methods to the `DockerWriteClient` interface in `handlers.go` and implement in `docker/client.go`.

### JSON-LD context

No changes needed — the context document uses `@vocab: "urn:cetacean:"` which automatically namespaces all `@type` values.

### Test pattern

Each endpoint gets tests following the existing `write_handlers_test.go` pattern:
- Mock write client with per-method function fields
- Test success path (valid body → 200 + updated resource in `NewDetailResponse` wrapper)
- Test not found (missing service → 404)
- Test invalid body (malformed JSON → 400)
- Test wrong Content-Type (PATCH endpoints → 415)

## Documentation Updates

- Update `OpsOperational` godoc in `config.go` to include placement, ports, update/rollback policy, log driver
- Update `docs/configuration.md` operations level section to list the new endpoints under tier 1
- Update `CLAUDE.md` env var table, architecture section, and key conventions as needed
- Update `api/openapi.yaml` with the new endpoint definitions
- Update `CHANGELOG.md`

## Out of Scope

- Frontend editor components (separate follow-up)
- Config/secret binding edits
- Mount/volume binding edits
- Network attachment edits
