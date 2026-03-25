---
title: API Reference
---

# Cetacean API Reference

Observability and management API for Docker Swarm Mode clusters.

Cetacean runs as a single binary that connects to the Docker socket, caches swarm state in memory, and serves it over HTTP. Read endpoints use GET; write operations use PUT, POST, PATCH, and DELETE gated by [operations level](configuration.md#operations-level). Authentication is [pluggable](authentication.md) via `CETACEAN_AUTH_MODE` (default: anonymous access).

The machine-readable OpenAPI spec is available at [`/api`](#api-documentation).

## Content Negotiation

Every resource URL serves JSON, HTML (the embedded SPA), or SSE depending on what the client asks for. No `/api/v1/` prefix -- versioning lives in the media type.

### Resolution order

1. **File extension** -- `.json` or `.html` appended to any path (highest priority)
2. **`Accept` header** -- standard content negotiation
3. **Default** -- `application/json` when `*/*` or no preference

### Supported types

| Accept value | Result |
|---|---|
| `application/json` | JSON (latest version) |
| `application/vnd.cetacean.v1+json` | JSON pinned to v1 |
| `text/html` | SPA |
| `text/event-stream` | SSE (only on endpoints that support it) |

All negotiated responses include `Vary: Accept`.

Requesting an unsupported type returns `406 Not Acceptable`.

```bash
# Force JSON via extension
curl http://localhost:9000/services.json

# Force JSON via Accept header
curl -H "Accept: application/json" http://localhost:9000/services

# Pin to API v1
curl -H "Accept: application/vnd.cetacean.v1+json" http://localhost:9000/services
```

## Common Query Parameters

List endpoints support these parameters:

| Parameter | Type | Default | Description |
|---|---|---|---|
| `limit` | int | 50 | Items per page (1-200) |
| `offset` | int | 0 | Starting position |
| `sort` | string | -- | Sort field (varies by resource) |
| `dir` | string | `asc` | Sort direction: `asc` or `desc` |
| `search` | string | -- | Case-insensitive substring match on name |
| `filter` | string | -- | [expr-lang](https://expr-lang.org/) expression (max 512 chars) |

```bash
# Page through services
curl "http://localhost:9000/services?limit=10&offset=20"

# Sort nodes by hostname descending
curl "http://localhost:9000/nodes?sort=hostname&dir=desc"

# Search configs by name
curl "http://localhost:9000/configs?search=nginx"

# Filter services with expr-lang
curl "http://localhost:9000/services?filter=name+contains+'web'"
```

### Sort fields by resource

| Resource | Sortable fields |
|---|---|
| Nodes | `hostname`, `role`, `status`, `availability` |
| Services | `name`, `mode` |
| Tasks | `state`, `service`, `node` |
| Stacks | `name` |
| Configs | `name`, `created`, `updated` |
| Secrets | `name`, `created`, `updated` |
| Networks | `name`, `driver`, `scope` |
| Volumes | `name`, `driver`, `scope` |

### Filter fields by resource

Filter expressions use [expr-lang](https://expr-lang.org/) syntax. The result must be boolean. Operators: `==`, `!=`, `<`, `>`, `<=`, `>=`, `contains`, `startsWith`, `endsWith`, `in`, `not in`, `&&`, `||`, `!`.

**Nodes**: `id`, `name` (hostname), `state` (`ready`/`down`/`unknown`), `role` (`manager`/`worker`), `availability` (`active`/`pause`/`drain`)

**Services**: `id`, `name`, `image`, `mode` (`replicated`/`global`), `stack`

**Tasks**: `id`, `state` (`new`/`allocated`/`pending`/`activating`/`running`/`deactivating`/`stopping`/`completed`/`failed`/`rejected`), `desired_state`, `image`, `exit_code`, `error`, `service` (ID), `node` (ID), `slot` (int)

**Configs**: `id`, `name`

**Secrets**: `id`, `name`

**Networks**: `id`, `name`, `driver`, `scope` (`swarm`/`local`)

**Volumes**: `name`, `driver`, `scope`

**Stacks**: `name`, `services` (count), `configs` (count), `secrets` (count), `networks` (count), `volumes` (count)

```bash
# Manager nodes that are ready
curl "http://localhost:9000/nodes?filter=role+%3D%3D+%22manager%22+%26%26+state+%3D%3D+%22ready%22"

# Failed tasks with errors
curl "http://localhost:9000/tasks?filter=state+%3D%3D+%22failed%22+%7C%7C+error+!%3D+%22%22"

# Stacks with more than 5 services
curl "http://localhost:9000/stacks?filter=services+>+5"
```

## Response Format

All responses use JSON-LD annotations (`@context`, `@id`, `@type`) for self-description.

### Collections

```json
{
  "@context": "/api/context.jsonld",
  "@type": "Collection",
  "items": [
    { "ID": "abc123", "Spec": { "..." : "..." } }
  ],
  "total": 42,
  "limit": 50,
  "offset": 0
}
```

Pagination links are provided via RFC 8288 `Link` headers:

```
Link: </services?limit=50&offset=50>; rel="next"
```

### Detail responses

```json
{
  "@context": "/api/context.jsonld",
  "@id": "/nodes/abc123",
  "@type": "Node",
  "node": { "..." : "..." },
  "services": [
    { "@id": "/services/def456", "name": "web" }
  ]
}
```

Detail responses for configs, secrets, networks, and volumes include a `services` array of cross-references to services that use the resource.

Task details include linked service and node references:

```json
{
  "@context": "/api/context.jsonld",
  "@id": "/tasks/abc123",
  "@type": "Task",
  "task": { "..." : "..." },
  "service": { "@id": "/services/def456", "name": "web" },
  "node": { "@id": "/nodes/ghi789", "hostname": "worker-1" }
}
```

## Errors

Error responses follow [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) (Problem Details) with Content-Type `application/problem+json`.

```json
{
  "@context": "/api/context.jsonld",
  "type": "about:blank",
  "title": "Not Found",
  "status": 404,
  "detail": "node abc123 not found",
  "instance": "/nodes/abc123",
  "requestId": "a1b2c3d4e5f6"
}
```

### Error codes

Every domain-specific error includes a stable error code in its `type` URI:

```json
{
  "@context": "/api/context.jsonld",
  "type": "/api/errors/SVC001",
  "title": "Service Version Conflict",
  "status": 409,
  "detail": "service was modified by another client",
  "instance": "/services/abc123/scale",
  "requestId": "a1b2c3d4e5f6"
}
```

The code is the last path segment of `type` (e.g. `SVC001`). Codes use a three-letter domain prefix followed by a three-digit number:

| Prefix | Domain |
|---|---|
| `API` | Protocol and content negotiation |
| `AUT` | Authentication |
| `OPS` | Operations level |
| `FLT` | Filter expressions |
| `SEA` | Search |
| `MTR` | Metrics / Prometheus |
| `LOG` | Log streaming |
| `SSE` | SSE connections |
| `ENG` | Docker Engine |
| `SWM` | Swarm operations |
| `PLG` | Plugin operations |
| `NOD` | Node operations |
| `SVC` | Service operations |
| `TSK` | Task operations |
| `STK` | Stack operations |
| `VOL` | Volume operations |
| `NET` | Network operations |
| `CFG` | Config operations |
| `SEC` | Secret operations |

Generic HTTP errors (no domain-specific code) use `"type": "about:blank"`.

Browse the full error reference at [`GET /api/errors`](#error-reference) or look up a single code at `GET /api/errors/{code}`.

## Caching

JSON responses include an `ETag` header (SHA-256 of the response body). Use `If-None-Match` for conditional requests:

```bash
# First request -- note the ETag
curl -v http://localhost:9000/services
# < ETag: "3a7f..."

# Conditional request
curl -H 'If-None-Match: "3a7f..."' http://localhost:9000/services
# < HTTP/1.1 304 Not Modified
```

Static resources (`/api`, `/api/context.jsonld`) return `Cache-Control: public, max-age=3600`.

SSE and streaming endpoints do not set caching headers.

## Real-Time Events (SSE)

Every resource endpoint supports SSE in addition to JSON. Send `Accept: text/event-stream` to any list or detail URL to open a per-resource event stream.

### Per-resource streams

List endpoints stream events filtered by resource type. Detail endpoints stream events for that specific resource.

```bash
# Stream all node events
curl -H "Accept: text/event-stream" http://localhost:9000/nodes

# Stream events for a specific service
curl -H "Accept: text/event-stream" http://localhost:9000/services/abc123

# Stream all task events
curl -H "Accept: text/event-stream" http://localhost:9000/tasks
```

This is the primary SSE mechanism -- the frontend uses per-resource streams for real-time updates on every page.

### Global event stream

`/events` provides a single stream of all resource changes:

```bash
curl -H "Accept: text/event-stream" http://localhost:9000/events
```

### Event format

Single events are sent with the resource type as the event name:

```
id: 1
event: service
data: {"@id":"/services/abc","@type":"Service","type":"service","action":"update","id":"abc","resource":{...}}
```

When multiple events arrive within the batch interval (default 100ms), they are sent as a `batch` event:

```
id: 2
event: batch
data: [{"@id":"/services/abc","@type":"Service","type":"service","action":"update","id":"abc","resource":{...}},...]
```

### Filtering

Use `?types=` to subscribe to specific resource types:

```bash
# Only service and node events
curl -H "Accept: text/event-stream" "http://localhost:9000/events?types=service,node"
```

Valid types: `node`, `service`, `task`, `config`, `secret`, `network`, `volume`, `stack`.

### Reconnection

The server assigns incrementing `id:` values to each event. EventSource clients automatically send `Last-Event-ID` on reconnect.

### Per-resource SSE

In addition to the global `/events` stream, every list and detail endpoint supports SSE via content negotiation. Request `Accept: text/event-stream` on any resource URL to receive updates scoped to that resource.

**List endpoints** stream events for all resources of that type:

```bash
# Stream all service changes
curl -H "Accept: text/event-stream" http://localhost:9000/services

# Stream all task changes
curl -H "Accept: text/event-stream" http://localhost:9000/tasks
```

**Detail endpoints** stream events for a single resource:

```bash
# Stream changes to one node
curl -H "Accept: text/event-stream" http://localhost:9000/nodes/abc123

# Stream changes to a stack (includes its services, tasks, configs, etc.)
curl -H "Accept: text/event-stream" http://localhost:9000/stacks/myapp
```

Events use the same format as `/events`. Stack streams include events for all member resources (services, tasks, configs, secrets, networks, volumes).

### Metrics SSE

The `/metrics` endpoint supports SSE for live-updating charts. Request `text/event-stream` to receive periodic metric updates instead of a one-shot JSON proxy response.

```bash
curl -H "Accept: text/event-stream" "http://localhost:9000/metrics?query=up&step=15&range=3600"
```

| Event | Description |
|---|---|
| `initial` | Full range query result on connect (same shape as Prometheus `query_range`). |
| `point` | Single instant query result appended at each tick. |

The stream runs instant queries on each tick interval and pushes new data points. Clients append `point` events to their existing data to build a rolling window.

### Connection limits

- **256** max concurrent SSE clients on `/events` and per-resource streams
- **128** max concurrent log stream connections
- **64** max concurrent metrics stream connections

When limits are reached, the server returns `429 Too Many Requests` with a `Retry-After: 5` header.

## Endpoint Reference

### Meta

No content negotiation. No discovery `Link` headers.

| Method | Path | Description |
|---|---|---|
| GET | `/-/health` | Health check. Returns version info. |
| GET | `/-/ready` | Readiness probe. 503 until first sync completes. |
| GET | `/-/metrics/status` | Monitoring auto-detection status (Prometheus, node-exporter, cAdvisor). |
| GET | `/-/metrics/labels` | Proxied Prometheus label names (optional `match[]` filter). |
| GET | `/-/metrics/labels/{name}` | Proxied Prometheus label values for a given label name. |
| GET | `/-/docker-latest-version` | Latest Docker Engine version (cached). |

```bash
curl http://localhost:9000/-/health
# {"status":"ok","version":"...","commit":"...","buildDate":"..."}

curl http://localhost:9000/-/ready
# {"status":"ready"}  (or 503 {"status":"not_ready"})
```

### Monitoring

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/metrics` | Proxied Prometheus query (content-negotiated: JSON, SSE, or HTML console). Instant vs range determined by `start`+`end`. | `query` (required), `time`, `start`, `end`, `step`, `range` |

Supports [SSE for live updates](#metrics-sse).

```bash
# Instant query
curl "http://localhost:9000/metrics?query=up"

# Range query
curl "http://localhost:9000/metrics?query=up&start=1700000000&end=1700003600&step=15"

# SSE stream
curl -H "Accept: text/event-stream" "http://localhost:9000/metrics?query=up&step=15&range=3600"
```

### Cluster

| Method | Path | Description |
|---|---|---|
| GET | `/cluster` | Cluster snapshot: node/service/task counts, resource totals. |
| GET | `/cluster/metrics` | CPU, memory, disk utilization (requires Prometheus). |
| GET | `/cluster/capacity` | Cluster resource capacity (max single-node CPU/memory, totals). |
| GET | `/disk-usage` | Disk usage summary by type (images, containers, volumes, build cache). |
| GET | `/plugins` | Installed Docker plugins. |
| GET | `/swarm` | Swarm inspect: join tokens, raft config, CA config. |
| GET | `/swarm/unlock-key` | Current swarm unlock key (when autolock enabled). |

```bash
curl http://localhost:9000/cluster
curl http://localhost:9000/cluster/metrics
```

### Swarm Write Operations

Write operations on the swarm configuration. Gated by [operations level](configuration.md#operations-level).

| Method | Path | Tier | Description |
|---|---|---|---|
| PATCH | `/swarm/orchestration` | 2 | Patch orchestration config (task history retention). |
| PATCH | `/swarm/raft` | 2 | Patch Raft config (snapshot interval, election/heartbeat ticks). |
| PATCH | `/swarm/dispatcher` | 2 | Patch dispatcher config (heartbeat period). |
| PATCH | `/swarm/ca` | 3 | Patch CA config (node cert expiry). |
| PATCH | `/swarm/encryption` | 3 | Toggle Raft data-at-rest encryption (autolock). |
| POST | `/swarm/rotate-token` | 3 | Rotate worker or manager join token. |
| POST | `/swarm/rotate-unlock-key` | 3 | Rotate swarm unlock key. |
| POST | `/swarm/force-rotate-ca` | 3 | Force CA certificate rotation. |

PATCH endpoints accept `application/merge-patch+json`.

```bash
# Update task history retention
curl -X PATCH -H "Content-Type: application/merge-patch+json" \
  -d '{"taskHistoryRetentionLimit": 10}' \
  http://localhost:9000/swarm/orchestration

# Rotate worker join token
curl -X POST -d '{"role": "worker"}' http://localhost:9000/swarm/rotate-token
```

### Nodes

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/nodes` | List nodes. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/nodes/{id}` | Node detail. | -- |
| GET | `/nodes/{id}/tasks` | Tasks running on a node. | -- |

```bash
curl http://localhost:9000/nodes
curl http://localhost:9000/nodes/abc123
curl http://localhost:9000/nodes/abc123/tasks
```

#### Node Write Operations

| Method | Path | Tier | Description |
|---|---|---|---|
| PUT | `/nodes/{id}/availability` | 3 | Set node availability (active, drain, pause). |
| GET | `/nodes/{id}/labels` | — | Get node labels as key/value map. |
| PATCH | `/nodes/{id}/labels` | 3 | Patch node labels (JSON Patch, RFC 6902). |
| GET | `/nodes/{id}/role` | — | Get node role (worker or manager). |
| PUT | `/nodes/{id}/role` | 3 | Promote or demote a node. |
| DELETE | `/nodes/{id}` | 3 | Remove a node from the swarm. |

```bash
# Drain a node
curl -X PUT -d '{"availability": "drain"}' http://localhost:9000/nodes/abc123/availability

# Get node labels
curl http://localhost:9000/nodes/abc123/labels

# Add a label via JSON Patch
curl -X PATCH -H "Content-Type: application/json-patch+json" \
  -d '[{"op": "add", "path": "/env", "value": "production"}]' \
  http://localhost:9000/nodes/abc123/labels
```

### Services

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/services` | List services. Includes `RunningTasks` count. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/services/{id}` | Service detail. | -- |
| GET | `/services/{id}/tasks` | Tasks for a service. | -- |
| GET | `/services/{id}/logs` | Service logs. Supports SSE for streaming. | `limit`, `after`, `before`, `stream` |

```bash
# List services
curl http://localhost:9000/services

# Get a specific service
curl http://localhost:9000/services/abc123

# Fetch recent logs (JSON)
curl http://localhost:9000/services/abc123/logs

# Stream logs via SSE
curl -H "Accept: text/event-stream" http://localhost:9000/services/abc123/logs

# Logs with filters
curl "http://localhost:9000/services/abc123/logs?limit=100&stream=stderr&after=2026-03-12T00:00:00Z"
```

#### Log parameters

| Parameter | Default | Description |
|---|---|---|
| `limit` | 500 | Max lines to return (1-10000). JSON mode only. |
| `after` | -- | RFC 3339 timestamp or Go duration. Lines after this time. |
| `before` | -- | RFC 3339 timestamp or Go duration. Lines before this time. JSON only. |
| `stream` | -- | Filter by `stdout` or `stderr`. |

SSE log streams use `Last-Event-ID` for reconnection (set to the timestamp of the last received line).

#### Service Write Operations — Tier 1 (Operational)

| Method | Path | Description |
|---|---|---|
| PUT | `/services/{id}/scale` | Set replica count. |
| PUT | `/services/{id}/image` | Update container image. |
| POST | `/services/{id}/rollback` | Rollback to previous spec. |
| POST | `/services/{id}/restart` | Force re-deploy (increments ForceUpdate). |

```bash
# Scale to 5 replicas
curl -X PUT -d '{"replicas": 5}' http://localhost:9000/services/abc123/scale

# Update image
curl -X PUT -d '{"image": "nginx:1.27"}' http://localhost:9000/services/abc123/image

# Rollback
curl -X POST http://localhost:9000/services/abc123/rollback

# Restart
curl -X POST http://localhost:9000/services/abc123/restart
```

#### Service Write Operations — Tier 2 (Configuration)

Sub-resource endpoints for reading and modifying individual service configuration aspects. GET endpoints are always available; write operations require operations level 2.

| Method | Path | Patch Type | Description |
|---|---|---|---|
| GET | `/services/{id}/env` | — | Get environment variables. |
| PATCH | `/services/{id}/env` | JSON Patch | Patch environment variables. |
| GET | `/services/{id}/labels` | — | Get service labels. |
| PATCH | `/services/{id}/labels` | JSON Patch | Patch service labels. |
| GET | `/services/{id}/resources` | — | Get CPU/memory reservations and limits. |
| PATCH | `/services/{id}/resources` | Merge Patch | Patch resource requirements. |
| GET | `/services/{id}/healthcheck` | — | Get healthcheck config. |
| PUT | `/services/{id}/healthcheck` | — | Replace healthcheck config. |
| PATCH | `/services/{id}/healthcheck` | Merge Patch | Patch healthcheck config. |
| GET | `/services/{id}/placement` | — | Get placement constraints. |
| PUT | `/services/{id}/placement` | — | Replace placement constraints. |
| GET | `/services/{id}/ports` | — | Get published port bindings. |
| PATCH | `/services/{id}/ports` | Merge Patch | Patch port bindings. |
| GET | `/services/{id}/update-policy` | — | Get rolling update config. |
| PATCH | `/services/{id}/update-policy` | Merge Patch | Patch rolling update config. |
| GET | `/services/{id}/rollback-policy` | — | Get rollback config. |
| PATCH | `/services/{id}/rollback-policy` | Merge Patch | Patch rollback config. |
| GET | `/services/{id}/log-driver` | — | Get log driver config. |
| PATCH | `/services/{id}/log-driver` | Merge Patch | Patch log driver config. |
| GET | `/services/{id}/configs` | — | Get config references. |
| PATCH | `/services/{id}/configs` | — | Replace config references. |
| GET | `/services/{id}/secrets` | — | Get secret references. |
| PATCH | `/services/{id}/secrets` | — | Replace secret references. |
| GET | `/services/{id}/networks` | — | Get network attachments. |
| PATCH | `/services/{id}/networks` | — | Replace network attachments. |
| GET | `/services/{id}/mounts` | — | Get mount configuration. |
| PATCH | `/services/{id}/mounts` | — | Replace mount configuration. |
| GET | `/services/{id}/container-config` | — | Get container-level config. |
| PATCH | `/services/{id}/container-config` | Merge Patch | Patch container-level config. |

JSON Patch endpoints require `Content-Type: application/json-patch+json`. Merge Patch endpoints require `Content-Type: application/merge-patch+json`. Mismatched content types return `415`.

```bash
# Get environment variables
curl http://localhost:9000/services/abc123/env

# Add an env var via JSON Patch
curl -X PATCH -H "Content-Type: application/json-patch+json" \
  -d '[{"op": "add", "path": "/DEBUG", "value": "true"}]' \
  http://localhost:9000/services/abc123/env

# Update resources via Merge Patch
curl -X PATCH -H "Content-Type: application/merge-patch+json" \
  -d '{"memoryLimit": 536870912}' \
  http://localhost:9000/services/abc123/resources
```

#### Service Write Operations — Tier 3 (Impactful)

| Method | Path | Description |
|---|---|---|
| PUT | `/services/{id}/mode` | Change service mode (replicated/global). |
| PUT | `/services/{id}/endpoint-mode` | Change endpoint mode (vip/dnsrr). |
| DELETE | `/services/{id}` | Remove a service from the swarm. |

```bash
# Switch to global mode
curl -X PUT -d '{"mode": "global"}' http://localhost:9000/services/abc123/mode

# Remove a service
curl -X DELETE http://localhost:9000/services/abc123
```

### Tasks

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/tasks` | List tasks. Enriched with `ServiceName`, `NodeHostname`. | `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/tasks/{id}` | Task detail with service and node cross-references. | -- |
| GET | `/tasks/{id}/logs` | Task logs. Supports SSE for streaming. | `limit`, `after`, `before`, `stream` |

```bash
curl http://localhost:9000/tasks
curl http://localhost:9000/tasks/abc123
curl -H "Accept: text/event-stream" http://localhost:9000/tasks/abc123/logs
```

#### Task Write Operations

| Method | Path | Tier | Description |
|---|---|---|---|
| DELETE | `/tasks/{id}` | 3 | Force-remove a task. |

### Stacks

Stacks are derived from `com.docker.stack.namespace` labels.

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/stacks` | List stacks. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/stacks/summary` | Stack summaries with resource usage (requires Prometheus). | -- |
| GET | `/stacks/{name}` | Stack detail: services, tasks, configs, secrets, networks, volumes. | -- |

```bash
curl http://localhost:9000/stacks
curl http://localhost:9000/stacks/summary
curl http://localhost:9000/stacks/myapp
```

#### Stack Write Operations

| Method | Path | Tier | Description |
|---|---|---|---|
| DELETE | `/stacks/{name}` | 3 | Remove all services in the stack. |

### Configs

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/configs` | List configs. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/configs/{id}` | Config detail with cross-referenced services. Data is base64-encoded. | -- |
| POST | `/configs` | Create a config. Tier 2. | -- |

```bash
curl http://localhost:9000/configs
curl http://localhost:9000/configs/abc123

# Create a config (data is base64-encoded)
curl -X POST -d '{"name": "my-config", "data": "aGVsbG8="}' http://localhost:9000/configs
```

### Secrets

Secret data is always redacted in API responses.

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/secrets` | List secrets. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/secrets/{id}` | Secret detail with cross-referenced services. | -- |
| POST | `/secrets` | Create a secret. Tier 2. | -- |

```bash
curl http://localhost:9000/secrets
curl http://localhost:9000/secrets/abc123

# Create a secret (data is base64-encoded)
curl -X POST -d '{"name": "my-secret", "data": "c3VwZXJzZWNyZXQ="}' http://localhost:9000/secrets
```

### Networks

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/networks` | List networks. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/networks/{id}` | Network detail with cross-referenced services. | -- |

```bash
curl http://localhost:9000/networks
curl http://localhost:9000/networks/abc123
```

### Volumes

Volumes are keyed by name, not ID.

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/volumes` | List volumes. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET | `/volumes/{name}` | Volume detail with cross-referenced services. | -- |

```bash
curl http://localhost:9000/volumes
curl http://localhost:9000/volumes/my-data
```

### Search

Cross-resource global search. Searches names, images, and labels across all resource types.

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/search` | Global search. | `q` (required), `limit` |

The `limit` parameter controls max results **per type** (default 3, max 1000). Set `limit=0` for up to 1000 per type.

Response is grouped by resource type. Services and tasks include a `state` field.

```bash
# Quick search (3 per type)
curl "http://localhost:9000/search?q=nginx"

# Full search (up to 1000 per type)
curl "http://localhost:9000/search?q=nginx&limit=0"
```

### History

Ring buffer of the last 10,000 resource change events.

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/history` | Recent changes. | `limit` (1-200, default 50), `type`, `resourceId` |

```bash
# Recent changes
curl http://localhost:9000/history

# Filter by type
curl "http://localhost:9000/history?type=service&limit=10"

# Changes for a specific resource
curl "http://localhost:9000/history?resourceId=abc123"
```

### Topology

| Method | Path | Description |
|---|---|---|
| GET | `/topology/networks` | Network topology: overlay networks and their connected services. |
| GET | `/topology/placement` | Placement topology: tasks grouped by node. |

```bash
curl http://localhost:9000/topology/networks
curl http://localhost:9000/topology/placement
```

### Profile

| Method | Path | Description |
|---|---|---|
| GET | `/profile` | Current user profile (content-negotiated, with ETag). |

Unlike `/auth/whoami`, this endpoint participates in content negotiation and includes ETag support.

```bash
curl http://localhost:9000/profile
```

### Events

| Method | Path | Description | Parameters |
|---|---|---|---|
| GET | `/events` | SSE-only. Real-time resource change stream. | `types` (comma-separated) |

Returns `406` for non-SSE requests. See [Real-Time Events](#real-time-events-sse) for details.

```bash
curl -H "Accept: text/event-stream" http://localhost:9000/events
curl -H "Accept: text/event-stream" "http://localhost:9000/events?types=service,task"
```

### Authentication

See [Authentication](authentication.md) for full details on each auth mode.

| Method | Path | Description |
|---|---|---|
| GET | `/auth/whoami` | Current identity. Returns `Cache-Control: no-store`. |
| GET | `/auth/login` | Initiate OIDC login flow (OIDC mode only). |
| GET | `/auth/callback` | OIDC callback (OIDC mode only; redirected by IdP). |
| POST | `/auth/logout` | Clear session, optionally redirect to IdP logout (OIDC mode only). |

```bash
curl http://localhost:9000/auth/whoami
# {"subject":"anonymous","displayName":"Anonymous","provider":"none"}
```

### API Documentation

| Method | Path | Description |
|---|---|---|
| GET | `/api` | OpenAPI spec (JSON) or interactive Scalar playground (HTML via browser). |
| GET | `/api/context.jsonld` | JSON-LD context document. |
| GET | `/api/scalar.js` | Embedded Scalar standalone JS bundle. |
| GET | `/api/errors` | List all error codes (JSON). Browser requests serve the SPA error reference page. |
| GET | `/api/errors/{code}` | Error code detail: title, HTTP status, description, and suggestion (JSON). |

```bash
# Download OpenAPI spec (JSON)
curl http://localhost:9000/api > openapi.json

# Open playground in browser
open http://localhost:9000/api
```

## Rate Limits

There is no general rate limiting. The only limits are on concurrent streaming connections:

| Resource | Limit | Exceeded response |
|---|---|---|
| SSE event clients (`/events` and per-resource streams) | 256 | `429` + `Retry-After: 5` |
| Log stream connections | 128 | `429` + `Retry-After: 5` |
| Metrics stream connections (`/metrics` SSE) | 64 | `429` + `Retry-After: 5` |

## Self-Discovery

Every response (except `/-/` meta endpoints) includes RFC 8631 `Link` headers:

```
Link: </api>; rel="service-desc", </api/context.jsonld>; rel="describedby"
```

- `rel="service-desc"` points to the OpenAPI spec
- `rel="describedby"` points to the JSON-LD context document

## Request ID

Every request gets an `X-Request-ID` header in the response. You can send your own via the `X-Request-ID` request header (max 64 chars, ASCII printable); otherwise one is generated automatically. The request ID appears in error responses as `requestId` and in server logs.
