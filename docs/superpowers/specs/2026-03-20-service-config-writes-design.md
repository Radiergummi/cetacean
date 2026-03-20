# Service Config Write Operations Design

## Overview

Add write endpoints for four service configuration sub-resources: placement, published ports, update policy, rollback policy, and log driver. All are tier 1 (operational) and backend-only (no frontend editors in this scope).

## Endpoints

| Resource        | GET                                  | Write                                             | Tier |
|-----------------|--------------------------------------|-------------------------------------------------|------|
| Placement       | `GET /services/{id}/placement`       | `PUT /services/{id}/placement`                   | 1    |
| Published ports | `GET /services/{id}/ports`           | `PATCH /services/{id}/ports` (merge patch)       | 1    |
| Update policy   | `GET /services/{id}/update-policy`   | `PATCH /services/{id}/update-policy` (merge patch) | 1  |
| Rollback policy | `GET /services/{id}/rollback-policy` | `PATCH /services/{id}/rollback-policy` (merge patch) | 1 |
| Log driver      | `GET /services/{id}/log-driver`      | `PATCH /services/{id}/log-driver` (merge patch)  | 1    |

All GET endpoints return JSON-LD wrapped responses with ETag support and serve the SPA for HTML via `contentNegotiated`. All write endpoints go through `tier1` middleware.

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

PUT replaces the entire placement object. The request body is the placement object itself (not wrapped). Omitting the field or sending `null` clears it.

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

### Rollback Policy

`@type`: `ServiceRollbackPolicy`

Same structure as update policy, at `@id` `/services/{id}/rollback-policy`, with key `rollbackPolicy`.

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

Note: merge-patching `Options` replaces the entire map (RFC 7396 — nested objects merge, but this is a flat string map so keys not present in the patch are removed). This matches how Docker itself handles option updates.

## Backend Pattern

Each endpoint follows the existing resources/healthcheck template:

### Docker client method

```
Inspect service → mutate spec field → ServiceUpdate (with version) → re-inspect
```

New methods on `Client`:
- `UpdateServicePlacement(ctx, id, *swarm.Placement) (swarm.Service, error)`
- `UpdateServicePorts(ctx, id, []swarm.PortConfig) (swarm.Service, error)`
- `UpdateServiceUpdatePolicy(ctx, id, *swarm.UpdateConfig) (swarm.Service, error)`
- `UpdateServiceRollbackPolicy(ctx, id, *swarm.UpdateConfig) (swarm.Service, error)`
- `UpdateServiceLogDriver(ctx, id, *swarm.Driver) (swarm.Service, error)`

### GET handler

Read from cache, extract sub-resource, wrap in `DetailResponse`, serve with `writeJSONWithETag`.

### Write handler

- **PUT** (placement): decode body as `swarm.Placement`, call client method, return updated state
- **PATCH** (all others): validate `Content-Type: application/merge-patch+json`, marshal current → `map[string]any`, unmarshal patch → `map[string]any`, apply `mergePatch()`, unmarshal back to typed struct, call client method, return updated state

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

### Test pattern

Each endpoint gets tests following the existing `write_handlers_test.go` pattern:
- Mock write client with per-method function fields
- Test success path (valid body → 200 + updated resource)
- Test not found (missing service → 404)
- Test invalid body (malformed JSON → 400)
- Test wrong Content-Type (PATCH endpoints → 415)

## Tier Classification

All five endpoints are tier 1 (operational). Update the operations level documentation to include them.

## Out of Scope

- Frontend editor components (separate follow-up)
- Config/secret binding edits
- Mount/volume binding edits
- Network attachment edits
