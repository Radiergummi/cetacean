# JSON-LD Response Consistency Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make all content-negotiated API endpoints return JSON-LD wrapped responses, and fix OpenAPI spec schemas to accurately document response shapes.

**Architecture:** Apply existing `NewDetailResponse` wrapper to handlers that currently return raw JSON. Update `writeMutationResponse` callers to use the same pattern as their GET counterparts. Fix OpenAPI spec schemas to match actual response shapes.

**Tech Stack:** Go, OpenAPI 3.1 YAML, TypeScript (frontend API client)

---

## Summary of Changes

### Go handler fixes (wrap in JSON-LD)

| Handler | File | Currently | Fix |
|---------|------|-----------|-----|
| `HandleProfile` | `health_handlers.go` | `writeCachedJSON` raw struct | `NewDetailResponse` |
| `HandleClusterMetrics` | `handlers.go` | `writeCachedJSON` raw struct | `NewDetailResponse` |
| `writeMutationResponse` (env/labels) | `write_handlers.go` | Writes raw `map[string]string` | `NewDetailResponse` |
| `writeMutationResponse` (container-config) | `write_handlers.go` | Writes raw struct | `NewDetailResponse` |
| `HandlePluginPrivileges` | `plugin_handlers.go` | `writeJSON` raw | `NewDetailResponse` |

### Frontend fixes (unwrap new JSON-LD responses)

| Call site | File | Currently expects | Fix |
|-----------|------|-------------------|-----|
| `api.whoami()` | `client.ts:392` | Raw `Identity` from `/profile` | Unwrap from `DetailResponse` (e.g. `.profile` or appropriate key) |
| `api.clusterMetrics()` | `client.ts:438` | Raw `ClusterMetrics` from `/cluster/metrics` | Unwrap from `DetailResponse` |
| `patch<Record<string, string>>` (env) | `client.ts:660` | Raw `map[string]string]` | Unwrap from `DetailResponse` |
| `patch<Record<string, string>>` (labels) | `client.ts:662,664,608,610` | Raw `map[string]string]` | Unwrap from `DetailResponse` |
| `patch<ContainerConfig>` | `client.ts:743` | Raw `ContainerConfig` | Unwrap from `DetailResponse` |
| Demo mock handlers | `demo/handlers.ts` | Raw responses for `/profile`, `/cluster/metrics` | Wrap in JSON-LD shape to match |

### OpenAPI spec fixes

| Endpoints | Issue | Fix |
|-----------|-------|-----|
| 5 swarm PATCH (orchestration, raft, dispatcher, ca, encryption) | `{"type": "object"}` with no properties | Add actual property schemas matching the Go response structs |
| 7 plugin write operations | Not documented at all | Add full operation definitions |
| Label PATCH responses (services, nodes, configs, secrets) | Schema doesn't reflect JSON-LD wrapper | Update to match `DetailResponse` shape |
| `GET /profile` | Missing JSON-LD wrapper in schema | Update schema |
| `GET /cluster/metrics` | Missing JSON-LD wrapper in schema | Update schema |

### Not changing

- `GET /auth/whoami` — auth endpoints are exempt from content negotiation by design
- Topology endpoints — graph structures, not standard resources
- Log streams, Prometheus proxy — streaming/passthrough protocols
- Removal responses — 204 No Content, no body

---

### Task 1: Fix `writeMutationResponse` to Use JSON-LD

The root cause for most PATCH inconsistencies. `writeMutationResponse` in `write_handlers.go` writes raw JSON. Change it to wrap in `NewDetailResponse` using the request path as `@id`.

**Files:**
- Modify: `internal/api/write_handlers.go`

- [ ] Find `writeMutationResponse` and update it to wrap the response in `NewDetailResponse`
- [ ] The `@type` should be derived from the sub-resource type (e.g., `ServiceEnv`, `NodeLabels`)
- [ ] Ensure `Prefer: return=minimal` (204) path is unaffected
- [ ] Run `go test ./internal/api/...` — fix any test assertions that expect raw JSON
- [ ] Commit

---

### Task 2: Fix `HandleProfile` and `HandleClusterMetrics`

**Files:**
- Modify: `internal/api/health_handlers.go` (`HandleProfile`)
- Modify: `internal/api/handlers.go` (`HandleClusterMetrics`)

- [ ] Wrap `HandleProfile` response in `NewDetailResponse` with `@type: "Profile"`
- [ ] Wrap `HandleClusterMetrics` response in `NewDetailResponse` with `@type: "ClusterMetrics"`
- [ ] Run `go test ./internal/api/...`
- [ ] Commit

---

### Task 3: Fix `HandlePluginPrivileges`

**Files:**
- Modify: `internal/api/plugin_handlers.go`

- [ ] Wrap `HandlePluginPrivileges` response in `NewDetailResponse`
- [ ] Run `go test ./internal/api/...`
- [ ] Commit

---

### Task 4: Update Frontend API Client and Demo Handlers

The Go handler changes wrap previously-raw responses in JSON-LD `DetailResponse`. The frontend's `mutationFetch` returns `res.json()` directly, so callers that expected raw payloads now receive `{ "@context": ..., "@id": ..., "@type": ..., "env": {...} }` instead of just `{...}`.

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/demo/handlers.ts`

- [ ] Update `api.whoami()` (line 392) to unwrap the profile from the JSON-LD envelope
- [ ] Update `api.clusterMetrics()` (line 438) to unwrap from JSON-LD envelope
- [ ] Update all `patch<Record<string, string>>` callers for env/labels (lines 608, 610, 660, 662, 664) to unwrap the map from the JSON-LD envelope — the key will match the sub-resource name (e.g. `env`, `labels`)
- [ ] Update `patch<ContainerConfig>` (line 743) to unwrap from JSON-LD envelope
- [ ] Update demo mock handlers in `demo/handlers.ts` for `/profile` (line 476) and `/cluster/metrics` (line 493) to return JSON-LD wrapped responses
- [ ] Run `npm run build` and `npx vitest run` from `frontend/`
- [ ] Commit

---

### Task 5: Update OpenAPI Spec — Swarm PATCH Schemas

**Files:**
- Modify: `api/openapi.yaml`

- [ ] Read the Go handler return types for each swarm PATCH endpoint to determine actual response shape
- [ ] Replace `{"type": "object"}` with accurate schemas for:
  - `/swarm/orchestration` PATCH 200
  - `/swarm/raft` PATCH 200
  - `/swarm/dispatcher` PATCH 200
  - `/swarm/ca` PATCH 200
  - `/swarm/encryption` PATCH 200
- [ ] Each should now include the JSON-LD `@context`, `@id`, `@type` envelope plus the actual payload properties
- [ ] Run `go test ./internal/api/...` (OpenAPI validation tests)
- [ ] Commit

---

### Task 6: Update OpenAPI Spec — Plugin Operations

**Files:**
- Modify: `api/openapi.yaml`

- [ ] Add operation definitions for all 7 plugin write endpoints:
  - `POST /plugins/privileges` (returns plugin privileges list)
  - `POST /plugins` (install, returns plugin detail)
  - `POST /plugins/{name}/enable` (returns 204)
  - `POST /plugins/{name}/disable` (returns 204)
  - `POST /plugins/{name}/upgrade` (returns plugin detail or 204)
  - `PATCH /plugins/{name}/settings` (returns plugin detail or 204)
  - `DELETE /plugins/{name}` (returns 204)
- [ ] Include request body schemas, response schemas, and error responses
- [ ] Run `go test ./internal/api/...` (OpenAPI validation tests)
- [ ] Commit

---

### Task 7: Update OpenAPI Spec — Remaining Schema Fixes

**Files:**
- Modify: `api/openapi.yaml`

- [ ] Update PATCH response schemas for label endpoints (services, nodes, configs, secrets) to reflect JSON-LD `DetailResponse` wrapper
- [ ] Update `GET /profile` response schema to reflect JSON-LD wrapper
- [ ] Update `GET /cluster/metrics` response schema to reflect JSON-LD wrapper
- [ ] Update container-config PATCH response schema
- [ ] Run `go test ./internal/api/...` (OpenAPI validation tests)
- [ ] Commit

---

### Task 8: Verify

- [ ] `go test ./...` — all tests pass
- [ ] `make check` — lint + fmt + test
- [ ] Spot-check a few endpoints with `curl` against a running instance (if available)
