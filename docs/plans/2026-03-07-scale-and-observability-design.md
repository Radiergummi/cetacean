# Cetacean Roadmap: Scale + Observability Breadth

Date: 2026-03-07

## Overview

Evolve Cetacean from a single-instance dashboard for small clusters into an observability platform that handles medium-sized Swarm clusters (~100 services, ~1000 tasks, ~10 nodes) with historical state tracking and topology visualization. Lay groundwork for future horizontal scaling without over-engineering now.

## Phases

| Phase | Focus | External dependencies added |
|-------|-------|-----------------------------|
| 1 | Scale single instance for medium clusters | None |
| 2 | Historical state / audit trail | None (in-memory ring buffer) |
| 3 | Network & placement topology | None (graph library in frontend) |
| 4 | Horizontal scaling (future) | Redis or NATS |

---

## Phase 1: Scale for Medium Clusters

### 1.1 Server-Side Pagination & Filtering

All list endpoints gain standardized query parameters:

| Parameter | Example | Notes |
|-----------|---------|-------|
| `limit` | `?limit=50` | Page size, default 50 |
| `offset` | `?offset=100` | Offset into sorted result |
| `sort` | `?sort=name` | Sort field |
| `dir` | `?dir=asc` | Sort direction (`asc`/`desc`) |
| `search` | `?search=nginx` | Already exists, keep as-is |
| `stack` | `?stack=monitoring` | Filter by stack namespace |
| `node` | `?node=abc123` | Filter by node ID |
| `status` | `?status=running` | Filter by status/state |

Response envelope changes from bare arrays to:

```json
{
  "items": [...],
  "total": 847
}
```

Implementation: pagination is slice windowing on the existing in-memory cache. Sort by taking a snapshot slice under RLock, sort it, then apply offset/limit. No architectural change.

Affected endpoints: `/api/services`, `/api/tasks`, `/api/nodes`, `/api/configs`, `/api/secrets`, `/api/networks`, `/api/volumes`, `/api/stacks`.

### 1.2 Frontend Virtual Scrolling

Replace HTML tables with virtualized rendering for list pages. Only DOM-render visible rows plus a small overscan buffer.

Options:
- **TanStack Virtual** — lightweight, headless, pairs well with existing table markup
- **react-window** — established, simple API

Pair with server-side pagination: frontend fetches pages as user scrolls (infinite scroll or explicit "load more").

### 1.3 SSE Event Batching

Batch SSE events into 100ms windows to reduce browser re-render pressure during high event rates (e.g., rolling update touching 50 tasks).

```
event: batch
data: [{"type":"task","action":"update","id":"abc",...},{"type":"task","action":"update","id":"def",...}]
```

Frontend SSE hook processes batch events the same as individual events, just iterates the array. Debounce React state updates to once per batch.

---

## Phase 2: Historical State / Audit Trail

### 2.1 Event Journal (In-Memory Ring Buffer)

Record every state mutation in a fixed-size ring buffer (configurable, default 10,000 entries). Each entry contains:

```go
type HistoryEntry struct {
    ID        uint64          `json:"id"`
    Timestamp time.Time       `json:"timestamp"`
    Type      string          `json:"type"`      // "service", "task", "node", etc.
    Action    string          `json:"action"`     // "set", "delete"
    ResourceID string         `json:"resourceId"`
    Name      string          `json:"name"`       // human-readable resource name
    Summary   string          `json:"summary"`    // e.g., "task state: running -> failed"
}
```

No before/after snapshots in v1 — just structured summaries to keep memory bounded. The summary is derived at write time (e.g., diff task state, detect image change on service update).

### 2.2 History API

| Endpoint | Returns |
|----------|---------|
| `GET /api/history` | Recent events, paginated. `?limit=50&offset=0` |
| `GET /api/history?type=service` | Filtered by resource type |
| `GET /api/history?resourceId=abc` | Events for a specific resource |

### 2.3 Frontend: Activity Feed

- **Cluster Overview**: "Recent Activity" panel below stat cards showing last 20 events with relative timestamps
- **Detail pages**: "History" tab showing events scoped to that resource ID
- Events displayed as a vertical timeline with icon per action type (created, updated, removed, failed)

### 2.4 Ephemeral by Design

This history is lost on restart. That's acceptable for Phase 2. When horizontal scaling (Phase 4) adds Redis/NATS, the journal moves to Redis Streams with TTL-based retention (e.g., 30 days), surviving restarts and shared across replicas.

---

## Phase 3: Network & Placement Topology

### 3.1 Topology API

Two new endpoints returning pre-computed graph structures:

**`GET /api/topology/networks`**

```json
{
  "nodes": [
    {"id": "svc-abc", "type": "service", "name": "nginx", "stack": "web", "replicas": 3}
  ],
  "edges": [
    {"source": "svc-abc", "target": "svc-def", "networks": ["overlay-1"]}
  ],
  "networks": [
    {"id": "overlay-1", "name": "web_default", "driver": "overlay"}
  ]
}
```

Services sharing an overlay network get an edge. Data source: services have `Endpoint.VirtualIPs` linking to network IDs, both already in cache.

**`GET /api/topology/placement`**

```json
{
  "nodes": [
    {
      "id": "node-1", "hostname": "worker-01", "role": "worker", "state": "ready",
      "tasks": [
        {"id": "task-1", "serviceId": "svc-abc", "serviceName": "nginx", "state": "running", "slot": 1}
      ]
    }
  ]
}
```

Tasks grouped by node, colored by service/stack. Data source: tasks have `NodeID` and `ServiceID`, both in cache.

### 3.2 Frontend Graph Visualization

New top-level nav item: **Topology**, with two tab views:

**Network view:**
- Force-directed graph (D3 force simulation or `@xyflow/react`)
- Service nodes as circles sized by replica count
- Edges for shared overlay networks, labeled with network name
- Color-grouped by stack
- Click a service node to navigate to its detail page
- Click an edge to highlight the shared network

**Placement view:**
- Cluster nodes as large rectangles (swim lanes or containers)
- Tasks as small circles inside, colored by service/stack
- Visual density shows load distribution across nodes
- Click a task to navigate to service detail

Both views: zoomable, pannable, with a legend. Real-time updates via SSE (graph re-layouts on topology changes).

### 3.3 Library Choice

Recommended: **D3 force layout** with React wrapper. Reasons:
- Full control over rendering and layout
- No heavy framework dependency
- Well-documented for network graph use cases
- Already used widely in observability tools (Grafana node graph, Kibana)

Alternative: `@xyflow/react` if we want built-in interaction primitives (drag, zoom, minimap) without manual D3 work. Heavier but faster to build.

Decision deferred to implementation.

---

## Phase 4: Horizontal Scaling (Future — Not Designed in Detail)

When the need arises:

- Add Redis or NATS as shared pub/sub layer
- Each replica watches Docker independently, builds own in-memory cache
- SSE events fan out via pub/sub so clients on any replica see all events
- Event journal moves to Redis Streams for persistent, shared history
- Replicas are stateless behind a load balancer
- Cluster membership is implicit (all replicas subscribe to same pub/sub channels)

---

## Out of Scope

| Area | Reason |
|------|--------|
| Authentication / authorization | Stays external (reverse proxy, OIDC) |
| Operational actions (restart, scale) | Separate future decision, changes security model |
| Custom PromQL dashboards | Grafana exists for ad-hoc queries |
| Distributed tracing | Requires instrumentation in user services |
| Centralized log aggregation | Loki exists; Cetacean stays with per-service Docker API logs |
