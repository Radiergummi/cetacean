# Collection Item JSON-LD + OpenAPI Spec Sync Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every collection item carry its own `@id` and `@type`, and bring the OpenAPI spec in sync with the JSON-LD envelopes the handlers actually return.

**Architecture:** Two parts. (1) Backend: extend `NewCollectionResponse` to wrap each item with a lightweight JSON-LD envelope (`@id`, `@type`, fields inlined) using a per-resource ID extractor. (2) Spec: update 32+ OpenAPI operation response schemas and 12 collection item schemas to reflect the real JSON-LD shape.

**Tech Stack:** Go, OpenAPI 3.1 YAML

---

## Part 1: Backend — Per-Item JSON-LD

### Task 1: Extend the Collection Envelope

Add a generic helper that wraps each item with `@id` + `@type`. The current `CollectionResponse[T]` returns items as raw `T` (e.g. `swarm.Node`). We need them to serialize with `@id` and `@type` at the top level.

**Files:**
- Modify: `internal/api/jsonld.go`

The approach: introduce a typed `Item` wrapper that carries an ID and a type name, and delegates field inlining to the underlying type via a custom `MarshalJSON` (similar to how `DetailResponse` handles `extra`).

```go
// Item wraps a collection entry with JSON-LD @id and @type fields.
// MarshalJSON produces: {"@id": "...", "@type": "...", <fields>}
type Item[T any] struct {
    id  string
    typ string
    val T
}

func (i Item[T]) MarshalJSON() ([]byte, error) {
    valBytes, err := json.Marshal(i.val)
    if err != nil {
        return nil, err
    }

    idJSON, err := json.Marshal(i.id)
    if err != nil {
        return nil, err
    }

    buf := make([]byte, 0, len(valBytes)+64)
    buf = append(buf, `{"@id":`...)
    buf = append(buf, idJSON...)
    buf = append(buf, `,"@type":"`...)
    buf = append(buf, i.typ...)
    buf = append(buf, `"`...)

    if len(valBytes) > 2 { // not "{}"
        buf = append(buf, ',')
        buf = append(buf, valBytes[1:len(valBytes)-1]...)
    }

    buf = append(buf, '}')
    return buf, nil
}
```

- [ ] Add the `Item[T]` type and its `MarshalJSON` method to `jsonld.go`
- [ ] Add a helper `wrapItems[T any](items []T, typ string, id func(T) string) []Item[T]` that maps a slice to `[]Item[T]`
- [ ] Unit test: a `[]swarm.Node` wrapped as `[]Item[swarm.Node]` serializes with `@id`, `@type`, plus all node fields at top level (no nesting under a key)
- [ ] Commit

---

### Task 2: Update the List Spec to Include an ID Extractor

**Files:**
- Modify: `internal/api/list.go`
- Modify: `internal/api/pagination.go`

The `listSpec[T]` already has `linkTemplate` (e.g. `/nodes/{id}`). Add an `idFunc func(T) string` field that extracts the resource ID, and derive the full `@id` by substituting it into the template.

Actually simpler: add an `itemType string` field and `idFunc func(T) string` to `listSpec[T]`. In `handleList`, after `applyPagination`, wrap items via `wrapItems` before writing.

- [ ] Add `itemType string` and `idFunc func(T) string` to `listSpec[T]` in `list.go`
- [ ] Update `handleList` to transform `resp.Items` from `[]T` to `[]Item[T]` using `wrapItems`
- [ ] Since `CollectionResponse[T]` is generic, you may need a `CollectionResponse[Item[T]]` at the write step — verify this compiles and serializes correctly
- [ ] Commit (tests may not pass yet until Task 3)

---

### Task 3: Wire Up All List Handlers

For every handler that uses `handleList`, add `itemType` and `idFunc` to the `listSpec`:

**Files:**
- Modify: `internal/api/node_handlers.go` — `HandleListNodes`: `itemType: "Node"`, `idFunc: func(n swarm.Node) string { return "/nodes/" + n.ID }`
- Modify: `internal/api/service_handlers.go` — `HandleListServices`: `itemType: "Service"`, id uses `svc.ID`
- Modify: `internal/api/task_handlers.go` — `HandleListTasks`: `itemType: "Task"`, id uses `t.ID`
- Modify: `internal/api/stack_handlers.go` — `HandleListStacks`: `itemType: "Stack"`, id uses stack name
- Modify: `internal/api/config_handlers.go` — `HandleListConfigs`: `itemType: "Config"`, id uses `cfg.ID`
- Modify: `internal/api/secret_handlers.go` — `HandleListSecrets`: `itemType: "Secret"`, id uses `sec.ID`
- Modify: `internal/api/network_handlers.go` — `HandleListNetworks`: `itemType: "Network"`, id uses `net.ID`
- Modify: `internal/api/volume_handlers.go` — `HandleListVolumes`: `itemType: "Volume"`, id uses volume name
- Modify: `internal/api/plugin_handlers.go` — `HandleListPlugins`: `itemType: "Plugin"`, id uses plugin name

Also non-`handleList` collections:
- `HandleGetNodeTasks`, `HandleGetServiceTasks` — build a Collection with `Item[Task]` entries
- `HandleHistory` — `itemType: "HistoryEntry"`, id uses entry ID
- `HandleStackSummary` — `itemType: "StackSummary"`, id uses stack name
- `HandleDiskUsage` — `itemType: "DiskUsageSummary"`, id per summary entry

- [ ] Update each handler to pass `itemType` and `idFunc`
- [ ] Run `go test ./internal/api/...` — existing list tests will need to assert on the new shape; update any that check raw item fields without unwrapping from the `Item` envelope
- [ ] Commit

---

### Task 4: Update Frontend to Handle New Collection Item Shape

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/api/types.ts` (if needed)
- Modify: `frontend/src/demo/handlers.ts` — update all mock collection responses

The frontend currently expects collection items to be raw `Node`, `Service`, etc. With the new shape, each item is `{"@id": "/nodes/abc", "@type": "Node", ID: "abc", Spec: {...}}` — the raw fields are still there, just alongside `@id` and `@type`. TypeScript should ignore the extra fields.

- [ ] Verify that existing collection consumers (hooks, pages) still work — the underlying fields are preserved, so in theory nothing breaks
- [ ] Update the TypeScript type definitions to include optional `"@id"` and `"@type"` if the frontend wants to use them
- [ ] Update demo mock handlers to include `@id` and `@type` in collection items to match the new production shape
- [ ] Run `npx vitest run` from `frontend/`
- [ ] Commit

---

## Part 2: OpenAPI Spec Sync

### Task 5: Fix Collection Response Schemas

The existing `CollectionEnvelope` schema needs a variant that documents per-item JSON-LD. Either:
- Add `@id` and `@type` to each individual item schema (e.g. `NodeListItem`, `ServiceListItem`)
- Or add a generic wrapper schema `ItemEnvelope` that can be combined with any type via `allOf`

**Files:**
- Modify: `api/openapi.yaml`

Recommended approach: add `@id` and `@type` as properties on each item schema (ServiceListItem, EnrichedTask, Stack, HistoryEntry, DiskUsageSummary, StackSummary, and inline Docker types for NodeCollection, ConfigCollection, SecretCollection, NetworkCollection, VolumeCollection, PluginCollection).

- [ ] For each of the 12 collection types identified in the audit, update the item schema to include `@id` (string) and `@type` (string) properties
- [ ] For inline Docker-type items (swarm.Node, swarm.Config, etc.), convert to a proper schema reference that includes `@id` and `@type`
- [ ] Commit

---

### Task 6: Fix 32 Operations Missing JSON-LD Envelopes in Spec

Every operation in the audit's Section 1 — the handlers already return JSON-LD, the spec just needs updating.

**Files:**
- Modify: `api/openapi.yaml`

Operations to fix (grouped):

**List endpoints** (wrap in `CollectionEnvelope` with correct item schema):
- GET /nodes, /services, /tasks, /stacks, /stacks/summary, /configs, /secrets, /networks, /volumes, /history, /disk-usage
- GET /nodes/{id}/tasks, /services/{id}/tasks

**Detail sub-resource GETs and PATCHes** (wrap in `DetailEnvelope` + payload key):
- GET/PATCH /services/{id}/resources — wrap with `{resources: ...}`
- GET/PUT /services/{id}/placement — wrap with `{placement: ...}`
- GET/PATCH /services/{id}/ports — wrap with `{ports: [...]}`
- GET/PATCH /services/{id}/update-policy — wrap with `{updatePolicy: ...}`
- GET/PATCH /services/{id}/rollback-policy — wrap with `{rollbackPolicy: ...}`
- GET/PATCH /services/{id}/log-driver — wrap with `{logDriver: ...}`
- GET /services/{id}/healthcheck — wrap with `{healthcheck: ...}`
- GET /services/{id}/configs — wrap with `{configs: [...]}`
- GET /services/{id}/secrets — wrap with `{secrets: [...]}`
- GET /services/{id}/networks — wrap with `{networks: [...]}`
- GET /services/{id}/container-config — wrap with `{containerConfig: ...}`

**Swarm operations:**
- GET /swarm/unlock-key — wrap with `{unlockKey: ...}`
- POST /swarm/rotate-unlock-key — wrap with `{unlockKey: ...}` (or 204 if handler returns that)
- POST /swarm/force-rotate-ca — verify what handler returns

- [ ] For each operation, replace the bare `$ref` or inline schema with an `allOf: [DetailEnvelope, {type: object, properties: {<key>: ...}}]` pattern
- [ ] Match each spec change to what the Go handler actually returns (read the handler first)
- [ ] Run `go test ./internal/api/...` — the OpenAPI spec validation test will catch most mismatches
- [ ] Commit in 2-3 logical chunks (list endpoints, service sub-resources, swarm ops)

---

### Task 7: Final Verification

- [ ] `go test ./...` — all tests pass
- [ ] `make check` — lint + fmt + test
- [ ] `npx vitest run` from `frontend/`
- [ ] Manually curl a list endpoint and a detail sub-resource endpoint — verify the JSON-LD shape matches the spec
