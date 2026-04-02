---
title: API Reference
description: REST endpoints, SSE streaming, query parameters, write operations, and error codes.
category: reference
tags: [api, rest, sse, json-ld, openapi]
---

# Cetacean API Reference

Observability and management API for Docker Swarm Mode clusters.

Cetacean runs as a single binary that connects to the Docker socket, caches swarm state in memory, and serves it over
HTTP. Read endpoints use GET; write operations use PUT, POST, PATCH, and DELETE gated
by [operations level](configuration.md#operations-level). Authentication is [pluggable](authentication.md) via
`auth.mode` (default: anonymous access).

The machine-readable OpenAPI spec is available at [`/api`](#api-documentation).

## Content Negotiation

Every resource URL serves JSON, HTML (the embedded SPA), or SSE depending on what the client asks for. No `/api/v1/`
prefix -- versioning lives in the media type.

### Resolution order

1. **File extension:** `.json` or `.html` appended to any path (the highest priority)
2. **`Accept` header:** standard content negotiation
3. **Default:** `application/json` when `*/*` or no preference

### Supported types

| Accept value                       | Result                                  |
|------------------------------------|-----------------------------------------|
| `application/json`                 | JSON (latest version)                   |
| `application/vnd.cetacean.v1+json` | JSON pinned to v1                       |
| `text/html`                        | SPA                                     |
| `text/event-stream`                | SSE (only on endpoints that support it) |
| `application/atom+xml`             | Atom feed (resource and history endpoints) |

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

## Atom Feeds

Resource list endpoints, resource detail endpoints, and the history, search, and recommendations endpoints all support Atom feeds. Request via `Accept: application/atom+xml` or append `.atom` to any supported path.

### Supported endpoints

All resource list and detail endpoints support Atom:

- `/nodes`, `/nodes/{id}`
- `/services`, `/services/{id}`
- `/tasks`, `/tasks/{id}`
- `/stacks`, `/stacks/{name}`
- `/configs`, `/configs/{id}`
- `/secrets`, `/secrets/{id}`
- `/networks`, `/networks/{id}`
- `/volumes`, `/volumes/{name}`
- `/events`, `/history`, `/search`, `/recommendations`

Endpoints that do not produce resource change data (write sub-resources, log streams, metrics, topology) return `406 Not Acceptable`.

### Pagination

Atom feeds use cursor-based pagination. The feed includes a `next` link when more entries are available:

| Parameter | Description                                      |
|-----------|--------------------------------------------------|
| `before`  | Return entries older than this cursor ID         |
| `limit`   | Number of entries per page (default 50, max 200) |

```bash
# Request an Atom feed
curl -H "Accept: application/atom+xml" http://localhost:9000/services

# Request via extension
curl http://localhost:9000/services.atom

# Page through history feed
curl "http://localhost:9000/history.atom?limit=50"
curl "http://localhost:9000/history.atom?before=<cursor-id>&limit=50"
```

## Pagination

List endpoints support two pagination mechanisms: query parameters and HTTP Range headers.

### Query parameters

| Parameter | Type   | Default | Description                                                    |
|-----------|--------|---------|----------------------------------------------------------------|
| `limit`   | int    | 50      | Items per page (1-200)                                         |
| `offset`  | int    | 0       | Starting position                                              |
| `sort`    | string | --      | Sort field (varies by resource)                                |
| `dir`     | string | `asc`   | Sort direction: `asc` or `desc`                                |
| `search`  | string | --      | Case-insensitive substring match on name                       |
| `filter`  | string | --      | [expr-lang](https://expr-lang.org/) expression (max 512 chars) |

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

### Range header pagination

List endpoints also accept `Range: items 0-24` for HTTP range-based pagination. Returns `206 Partial Content` with
`Content-Range: items 0-24/142`. When both query parameters and Range are present, query parameters take precedence.

### Sort fields by resource

| Resource | Sortable fields                              |
|----------|----------------------------------------------|
| Nodes    | `hostname`, `role`, `status`, `availability` |
| Services | `name`, `mode`                               |
| Tasks    | `state`, `service`, `node`                   |
| Stacks   | `name`                                       |
| Configs  | `name`, `created`, `updated`                 |
| Secrets  | `name`, `created`, `updated`                 |
| Networks | `name`, `driver`, `scope`                    |
| Volumes  | `name`, `driver`, `scope`                    |

### Filter fields by resource

Filter expressions use [expr-lang](https://expr-lang.org/) syntax. The result must be boolean. Operators: `==`, `!=`,
`<`, `>`, `<=`, `>=`, `contains`, `startsWith`, `endsWith`, `in`, `not in`, `&&`, `||`, `!`.

**Nodes**: `id`, `name` (hostname), `state` (`ready`/`down`/`unknown`), `role` (`manager`/`worker`), `availability` (
`active`/`pause`/`drain`)

**Services**: `id`, `name`, `image`, `mode` (`replicated`/`global`), `stack`

**Tasks**: `id`, `state` (`new`/`allocated`/`pending`/`activating`/`running`/`deactivating`/`stopping`/`completed`/
`failed`/`rejected`), `desired_state`, `image`, `exit_code`, `error`, `service` (ID), `node` (ID), `slot` (int)

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

All responses include JSON-LD annotations (`@context`, `@id`, `@type`) for self-description. Collection responses
wrap items in `{ items, total, limit, offset }` with RFC 8288 `Link` headers for pagination. Detail responses wrap
the resource with cross-references (e.g., services using a config, or the service and node for a task).

## Errors

Error responses follow [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) (Problem Details) with Content-Type
`application/problem+json`.

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

The code is the last path segment of `type` (e.g. `SVC001`). Codes use a three-letter domain prefix followed by a
three-digit number:

| Prefix | Domain                           |
|--------|----------------------------------|
| `API`  | Protocol and content negotiation |
| `AUT`  | Authentication                   |
| `OPS`  | Operations level                 |
| `FLT`  | Filter expressions               |
| `SEA`  | Search                           |
| `MTR`  | Metrics / Prometheus             |
| `LOG`  | Log streaming                    |
| `ACL`  | Authorization (RBAC)             |
| `SSE`  | SSE connections                  |
| `ENG`  | Docker Engine                    |
| `SWM`  | Swarm operations                 |
| `PLG`  | Plugin operations                |
| `NOD`  | Node operations                  |
| `SVC`  | Service operations               |
| `TSK`  | Task operations                  |
| `STK`  | Stack operations                 |
| `VOL`  | Volume operations                |
| `NET`  | Network operations               |
| `CFG`  | Config operations                |
| `SEC`  | Secret operations                |

Generic HTTP errors (no domain-specific code) use `"type": "about:blank"`.

Browse the error reference interactively at [`GET /api/errors`](#api-documentation) or look up a single code at
`GET /api/errors/{code}`.

### Common error scenarios

**Version conflicts (409):** All Write endpoints use Docker's optimistic concurrency. If the resource was modified by
another client between your read and write, the server returns `409 Conflict` with a `SVC001`, `NOD002`, or similar
code.
Re-read the resource and retry.

**Operations level (403):** Requests to endpoints above the
configured [operations level](configuration.md#operations-level)
return `403` with code `OPS001`.

**Authorization denied (403):** When [ACL](authorization.md) is active, read access denied returns `ACL001` and write
access denied returns `ACL002`. The response includes the resource and permission that was checked.

**Unsupported patch type (415):** PATCH endpoints validate `Content-Type`. Sending `application/json` instead of
`application/json-patch+json` or `application/merge-patch+json` returns `415 Unsupported Media Type`.

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

Detail endpoints also return `Last-Modified` based on the resource's update timestamp. Use `If-Modified-Since` for
conditional requests alongside or instead of ETags.

## Response Headers

Beyond standard caching headers, Cetacean sets several headers to help clients discover capabilities:

**`Allow`:** GET and HEAD responses include an `Allow` header listing the HTTP methods available for that resource,
based on the current [operations level](configuration.md#operations-level) and [ACL](authorization.md) permissions. A
client can inspect this before attempting a write operation.

**`Accept-Patch`:** Resources that support PATCH include `Accept-Patch` listing the accepted content types
(`application/json-patch+json`, `application/merge-patch+json`, or both). Present only when the operations level and
ACL permit write operations.

**`Prefer: return=minimal`:** Write endpoints honor RFC 7240 `Prefer: return=minimal`. When set, successful writes
return `204 No Content` instead of the updated resource. The response includes `Preference-Applied: return=minimal`.

Standard security headers (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Content-Security-Policy`)
are set on all responses. HSTS is added when TLS is enabled.

## Real-Time Events (SSE)

Every resource endpoint supports SSE in addition to JSON. Send `Accept: text/event-stream` to any list or detail URL to
open a per-resource event stream.

### Per-resource streams

List Endpoints stream events filtered by resource type. Detail Endpoints stream events for a single resource. Stack
streams include events for all member resources (services, tasks, configs, secrets, networks, volumes).

```bash
# Stream all node events
curl -H "Accept: text/event-stream" http://localhost:9000/nodes

# Stream events for a specific service
curl -H "Accept: text/event-stream" http://localhost:9000/services/abc123

# Stream changes to a stack (includes member resources)
curl -H "Accept: text/event-stream" http://localhost:9000/stacks/myapp
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

### Keepalive

The server sends SSE comment lines (`:keepalive`) on idle connections to prevent proxies and load balancers from closing
them. This is transparent to EventSource clients.

### Reconnection and Replay

The server assigns incrementing `id:` values to each event. EventSource clients automatically send `Last-Event-ID` on
reconnect, and the server replays missed events. If the requested ID is too old, the server sends a `sync` event to
tell the client to do a full reload.

### Metrics SSE

The `/metrics` endpoint supports SSE for live-updating charts. Request `text/event-stream` to receive periodic metric
updates instead of a one-shot JSON proxy response.

```bash
curl -H "Accept: text/event-stream" "http://localhost:9000/metrics?query=up&step=15&range=3600"
```

| Event     | Description                                                                  |
|-----------|------------------------------------------------------------------------------|
| `initial` | Full range query result on connect (same shape as Prometheus `query_range`). |
| `point`   | Single instant query result appended at each tick.                           |

The stream runs instant queries on each tick interval and pushes new data points. Clients append `point` events to their
existing data to build a rolling window.

### Connection Limits

SSE, log stream, and metrics stream connections are capped. When a limit is reached, the server returns
`429 Too Many Requests` with a `Retry-After` header.

## Endpoint Reference

### Meta

No content negotiation. No discovery `Link` headers.

| Method | Path                       | Description                                                             |
|--------|----------------------------|-------------------------------------------------------------------------|
| GET    | `/-/health`                | Health check. Returns version info.                                     |
| GET    | `/-/ready`                 | Readiness probe. 503 until first sync completes.                        |
| GET    | `/-/metrics`               | Cetacean's own Prometheus metrics (disabled via `server.self_metrics`). |
| GET    | `/metrics/status`          | Monitoring auto-detection status (Prometheus, node-exporter, cAdvisor). |
| GET    | `/metrics/labels`          | Proxied Prometheus label names (optional `match[]` filter).             |
| GET    | `/metrics/labels/{name}`   | Proxied Prometheus label values for a given label name.                 |
| GET    | `/-/docker-latest-version` | Latest Docker Engine version (cached).                                  |

### Monitoring

| Method | Path       | Description                                                                                                              | Parameters                                                  |
|--------|------------|--------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------------------|
| GET    | `/metrics` | Proxied Prometheus query (content-negotiated: JSON, SSE, or HTML console). Instant vs range determined by `start`+`end`. | `query` (required), `time`, `start`, `end`, `step`, `range` |

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

| Method | Path                | Description                                                            |
|--------|---------------------|------------------------------------------------------------------------|
| GET    | `/cluster`          | Cluster snapshot: node/service/task counts, resource totals.           |
| GET    | `/cluster/metrics`  | CPU, memory, disk utilization (requires Prometheus).                   |
| GET    | `/cluster/capacity` | Cluster resource capacity (max single-node CPU/memory, totals).        |
| GET    | `/disk-usage`       | Disk usage summary by type (images, containers, volumes, build cache). |
| GET    | `/swarm`            | Swarm inspect: join tokens, raft config, CA config.                    |
| GET    | `/swarm/unlock-key` | Current swarm unlock key (when autolock enabled).                      |

### Swarm Write Operations

Write operations on the swarm configuration. Gated by [operations level](configuration.md#operations-level).

| Method | Path                       | Tier | Description                                                      |
|--------|----------------------------|------|------------------------------------------------------------------|
| PATCH  | `/swarm/orchestration`     | 2    | Patch orchestration config (task history retention).             |
| PATCH  | `/swarm/raft`              | 2    | Patch Raft config (snapshot interval, election/heartbeat ticks). |
| PATCH  | `/swarm/dispatcher`        | 2    | Patch dispatcher config (heartbeat period).                      |
| PATCH  | `/swarm/ca`                | 3    | Patch CA config (node cert expiry).                              |
| PATCH  | `/swarm/encryption`        | 3    | Toggle Raft data-at-rest encryption (autolock).                  |
| POST   | `/swarm/rotate-token`      | 3    | Rotate worker or manager join token.                             |
| POST   | `/swarm/rotate-unlock-key` | 3    | Rotate swarm unlock key.                                         |
| POST   | `/swarm/force-rotate-ca`   | 3    | Force CA certificate rotation.                                   |
| POST   | `/swarm/unlock`            | 3    | Unlock the swarm.                                                |

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

| Method | Path                | Description              | Parameters                                           |
|--------|---------------------|--------------------------|------------------------------------------------------|
| GET    | `/nodes`            | List nodes.              | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/nodes/{id}`       | Node detail.             | --                                                   |
| GET    | `/nodes/{id}/tasks` | Tasks running on a node. | --                                                   |

#### Node Write Operations

| Method | Path                       | Tier | Description                                    |
|--------|----------------------------|------|------------------------------------------------|
| PUT    | `/nodes/{id}/availability` | 3    | Set node availability (active, drain, pause).  |
| GET    | `/nodes/{id}/labels`       | —    | Get node labels as key/value map.              |
| PATCH  | `/nodes/{id}/labels`       | 3    | Patch node labels (JSON Patch or Merge Patch). |
| GET    | `/nodes/{id}/role`         | —    | Get node role (worker or manager).             |
| PUT    | `/nodes/{id}/role`         | 3    | Promote or demote a node.                      |
| DELETE | `/nodes/{id}`              | 3    | Remove a node from the swarm.                  |

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

| Method | Path                   | Description                                   | Parameters                                           |
|--------|------------------------|-----------------------------------------------|------------------------------------------------------|
| GET    | `/services`            | List services. Includes `RunningTasks` count. | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/services/{id}`       | Service detail.                               | --                                                   |
| GET    | `/services/{id}/tasks` | Tasks for a service.                          | --                                                   |
| GET    | `/services/{id}/logs`  | Service logs. Supports SSE for streaming.     | `limit`, `after`, `before`, `stream`                 |

```bash
# Stream logs via SSE
curl -H "Accept: text/event-stream" http://localhost:9000/services/abc123/logs

# Logs with filters
curl "http://localhost:9000/services/abc123/logs?limit=100&stream=stderr&after=1h"
```

#### Log parameters

| Parameter | Default | Description                                                           |
|-----------|---------|-----------------------------------------------------------------------|
| `limit`   | 500     | Max lines to return (1-10000). JSON mode only.                        |
| `after`   | --      | RFC 3339 timestamp or Go duration. Lines after this time.             |
| `before`  | --      | RFC 3339 timestamp or Go duration. Lines before this time. JSON only. |
| `stream`  | --      | Filter by `stdout` or `stderr`.                                       |

SSE log streams use `Last-Event-ID` for reconnection (set to the timestamp of the last received line).

#### Service Write Operations — Tier 1 (Operational)

| Method | Path                      | Description                               |
|--------|---------------------------|-------------------------------------------|
| PUT    | `/services/{id}/scale`    | Set replica count.                        |
| PUT    | `/services/{id}/image`    | Update container image.                   |
| POST   | `/services/{id}/rollback` | Rollback to previous spec.                |
| POST   | `/services/{id}/restart`  | Force re-deploy (increments ForceUpdate). |

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

Sub-resource endpoints for reading and modifying individual service configuration aspects. GET endpoints are always
available; write operations require operations level 2.

| Method | Path                              | Patch Type                | Description                             |
|--------|-----------------------------------|---------------------------|-----------------------------------------|
| GET    | `/services/{id}/env`              | —                         | Get environment variables.              |
| PATCH  | `/services/{id}/env`              | JSON Patch or Merge Patch | Patch environment variables.            |
| GET    | `/services/{id}/labels`           | —                         | Get service labels.                     |
| PATCH  | `/services/{id}/labels`           | JSON Patch or Merge Patch | Patch service labels.                   |
| GET    | `/services/{id}/resources`        | —                         | Get CPU/memory reservations and limits. |
| PATCH  | `/services/{id}/resources`        | Merge Patch               | Patch resource requirements.            |
| GET    | `/services/{id}/healthcheck`      | —                         | Get healthcheck config.                 |
| PUT    | `/services/{id}/healthcheck`      | —                         | Replace healthcheck config.             |
| PATCH  | `/services/{id}/healthcheck`      | Merge Patch               | Patch healthcheck config.               |
| GET    | `/services/{id}/placement`        | —                         | Get placement constraints.              |
| PUT    | `/services/{id}/placement`        | —                         | Replace placement constraints.          |
| GET    | `/services/{id}/ports`            | —                         | Get published port bindings.            |
| PATCH  | `/services/{id}/ports`            | Merge Patch               | Patch port bindings.                    |
| GET    | `/services/{id}/update-policy`    | —                         | Get rolling update config.              |
| PATCH  | `/services/{id}/update-policy`    | Merge Patch               | Patch rolling update config.            |
| GET    | `/services/{id}/rollback-policy`  | —                         | Get rollback config.                    |
| PATCH  | `/services/{id}/rollback-policy`  | Merge Patch               | Patch rollback config.                  |
| GET    | `/services/{id}/log-driver`       | —                         | Get log driver config.                  |
| PATCH  | `/services/{id}/log-driver`       | Merge Patch               | Patch log driver config.                |
| GET    | `/services/{id}/configs`          | —                         | Get config references.                  |
| PATCH  | `/services/{id}/configs`          | Merge Patch               | Replace config references.              |
| GET    | `/services/{id}/secrets`          | —                         | Get secret references.                  |
| PATCH  | `/services/{id}/secrets`          | Merge Patch               | Replace secret references.              |
| GET    | `/services/{id}/networks`         | —                         | Get network attachments.                |
| PATCH  | `/services/{id}/networks`         | Merge Patch               | Replace network attachments.            |
| GET    | `/services/{id}/mounts`           | —                         | Get mount configuration.                |
| PATCH  | `/services/{id}/mounts`           | Merge Patch               | Replace mount configuration.            |
| GET    | `/services/{id}/container-config` | —                         | Get container-level config.             |
| PATCH  | `/services/{id}/container-config` | Merge Patch               | Patch container-level config.           |

JSON Patch endpoints require `Content-Type: application/json-patch+json`. Merge Patch endpoints require
`Content-Type: application/merge-patch+json`. Mismatched content types return `415`.

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

| Method | Path                           | Description                              |
|--------|--------------------------------|------------------------------------------|
| PUT    | `/services/{id}/mode`          | Change service mode (replicated/global). |
| PUT    | `/services/{id}/endpoint-mode` | Change endpoint mode (vip/dnsrr).        |
| DELETE | `/services/{id}`               | Remove a service from the swarm.         |

```bash
# Switch to global mode
curl -X PUT -d '{"mode": "global"}' http://localhost:9000/services/abc123/mode

# Remove a service
curl -X DELETE http://localhost:9000/services/abc123
```

### Tasks

| Method | Path               | Description                                              | Parameters                                 |
|--------|--------------------|----------------------------------------------------------|--------------------------------------------|
| GET    | `/tasks`           | List tasks. Enriched with `ServiceName`, `NodeHostname`. | `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/tasks/{id}`      | Task detail with service and node cross-references.      | --                                         |
| GET    | `/tasks/{id}/logs` | Task logs. Supports SSE for streaming.                   | `limit`, `after`, `before`, `stream`       |

#### Task Write Operations

| Method | Path          | Tier | Description          |
|--------|---------------|------|----------------------|
| DELETE | `/tasks/{id}` | 3    | Force-remove a task. |

### Stacks

Stacks are derived from `com.docker.stack.namespace` labels.

| Method | Path              | Description                                                         | Parameters                                           |
|--------|-------------------|---------------------------------------------------------------------|------------------------------------------------------|
| GET    | `/stacks`         | List stacks.                                                        | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/stacks/summary` | Stack summaries with resource usage (requires Prometheus).          | --                                                   |
| GET    | `/stacks/{name}`  | Stack detail: services, tasks, configs, secrets, networks, volumes. | --                                                   |

#### Stack Write Operations

| Method | Path             | Tier | Description                       |
|--------|------------------|------|-----------------------------------|
| DELETE | `/stacks/{name}` | 3    | Remove all services in the stack. |

### Configs

| Method | Path                   | Description                                                           | Parameters                                           |
|--------|------------------------|-----------------------------------------------------------------------|------------------------------------------------------|
| GET    | `/configs`             | List configs.                                                         | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/configs/{id}`        | Config detail with cross-referenced services. Data is base64-encoded. | --                                                   |
| GET    | `/configs/{id}/labels` | Get config labels.                                                    | --                                                   |
| PATCH  | `/configs/{id}/labels` | Patch config labels. Tier 2.                                          | --                                                   |
| POST   | `/configs`             | Create a config. Tier 2.                                              | --                                                   |
| DELETE | `/configs/{id}`        | Remove a config. Tier 3.                                              | --                                                   |

### Secrets

Secret data is always redacted in API responses.

| Method | Path                   | Description                                   | Parameters                                           |
|--------|------------------------|-----------------------------------------------|------------------------------------------------------|
| GET    | `/secrets`             | List secrets.                                 | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/secrets/{id}`        | Secret detail with cross-referenced services. | --                                                   |
| GET    | `/secrets/{id}/labels` | Get secret labels.                            | --                                                   |
| PATCH  | `/secrets/{id}/labels` | Patch secret labels. Tier 2.                  | --                                                   |
| POST   | `/secrets`             | Create a secret. Tier 2.                      | --                                                   |
| DELETE | `/secrets/{id}`        | Remove a secret. Tier 3.                      | --                                                   |

### Networks

| Method | Path             | Description                                    | Parameters                                           |
|--------|------------------|------------------------------------------------|------------------------------------------------------|
| GET    | `/networks`      | List networks.                                 | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/networks/{id}` | Network detail with cross-referenced services. | --                                                   |
| DELETE | `/networks/{id}` | Remove a network. Tier 3.                      | --                                                   |

### Volumes

Volumes are keyed by name, not ID.

| Method | Path              | Description                                   | Parameters                                           |
|--------|-------------------|-----------------------------------------------|------------------------------------------------------|
| GET    | `/volumes`        | List volumes.                                 | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/volumes/{name}` | Volume detail with cross-referenced services. | --                                                   |
| DELETE | `/volumes/{name}` | Remove a volume. Tier 3.                      | --                                                   |

### Plugins

| Method | Path              | Description             | Parameters |
|--------|-------------------|-------------------------|------------|
| GET    | `/plugins`        | List installed plugins. | --         |
| GET    | `/plugins/{name}` | Plugin detail.          | --         |

#### Plugin Write Operations

| Method | Path                       | Tier | Description                                              |
|--------|----------------------------|------|----------------------------------------------------------|
| POST   | `/plugins/privileges`      | 3    | Request plugin privileges.                               |
| POST   | `/plugins`                 | 3    | Install a plugin.                                        |
| POST   | `/plugins/{name}/enable`   | 2    | Enable a plugin.                                         |
| POST   | `/plugins/{name}/disable`  | 2    | Disable a plugin.                                        |
| PATCH  | `/plugins/{name}/settings` | 2    | Update plugin settings (`application/merge-patch+json`). |
| POST   | `/plugins/{name}/upgrade`  | 3    | Upgrade a plugin.                                        |
| DELETE | `/plugins/{name}`          | 3    | Remove a plugin.                                         |

### Search

Cross-resource global search. Searches names, images, and labels across all resource types.

| Method | Path      | Description    | Parameters              |
|--------|-----------|----------------|-------------------------|
| GET    | `/search` | Global search. | `q` (required), `limit` |

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

| Method | Path       | Description     | Parameters                                        |
|--------|------------|-----------------|---------------------------------------------------|
| GET    | `/history` | Recent changes. | `limit` (1-200, default 50), `type`, `resourceId` |

```bash
# Recent changes
curl http://localhost:9000/history

# Filter by type
curl "http://localhost:9000/history?type=service&limit=10"

# Changes for a specific resource
curl "http://localhost:9000/history?resourceId=abc123"
```

### Topology

| Method | Path                  | Description                                                      |
|--------|-----------------------|------------------------------------------------------------------|
| GET    | `/topology/networks`  | Network topology: overlay networks and their connected services. |
| GET    | `/topology/placement` | Placement topology: tasks grouped by node.                       |

### Recommendations

| Method | Path               | Description                                                                               |
|--------|--------------------|-------------------------------------------------------------------------------------------|
| GET    | `/recommendations` | All active cluster health recommendations, sorted by severity. Includes severity summary. |

See [Recommendations](recommendations.md) for categories and configuration.

### Profile

| Method | Path       | Description                                           |
|--------|------------|-------------------------------------------------------|
| GET    | `/profile` | Current user profile (content-negotiated, with ETag). |

Unlike `/auth/whoami`, this endpoint participates in content negotiation and includes ETag support.

### Events

| Method | Path      | Description                                 | Parameters                |
|--------|-----------|---------------------------------------------|---------------------------|
| GET    | `/events` | SSE-only. Real-time resource change stream. | `types` (comma-separated) |

Returns `406` for non-SSE requests. See [Real-Time Events](#real-time-events-sse) for details.

### Authentication

See [Authentication](authentication.md) for full details on each auth mode.

| Method | Path             | Description                                                        |
|--------|------------------|--------------------------------------------------------------------|
| GET    | `/auth/whoami`   | Current identity. Returns `Cache-Control: no-store`.               |
| GET    | `/auth/login`    | Initiate OIDC login flow (OIDC mode only).                         |
| GET    | `/auth/callback` | OIDC callback (OIDC mode only; redirected by IdP).                 |
| POST   | `/auth/logout`   | Clear session, optionally redirect to IdP logout (OIDC mode only). |

### API Documentation

| Method | Path                  | Description                                                                       |
|--------|-----------------------|-----------------------------------------------------------------------------------|
| GET    | `/api`                | OpenAPI spec (JSON) or interactive Scalar playground (HTML via browser).          |
| GET    | `/api/context.jsonld` | JSON-LD context document.                                                         |
| GET    | `/api/scalar.js`      | Embedded Scalar standalone JS bundle.                                             |
| GET    | `/api/errors`         | List all error codes (JSON). Browser requests serve the SPA error reference page. |
| GET    | `/api/errors/{code}`  | Error code detail: title, HTTP status, description, and suggestion (JSON).        |

```bash
# Download OpenAPI spec (JSON)
curl http://localhost:9000/api > openapi.json

# Open playground in browser
open http://localhost:9000/api
```

## Rate Limits

There is no general rate limiting. The only limits are on concurrent streaming connections:

| Resource                                               | Limit | Exceeded response        |
|--------------------------------------------------------|-------|--------------------------|
| SSE event clients (`/events` and per-resource streams) | 256   | `429` + `Retry-After: 5` |
| Log stream connections                                 | 128   | `429` + `Retry-After: 5` |
| Metrics stream connections (`/metrics` SSE)            | 64    | `429` + `Retry-After: 5` |

## Self-Discovery

Every response (except `/-/` meta endpoints) includes RFC 8631 `Link` headers:

```
Link: </api>; rel="service-desc", </api/context.jsonld>; rel="describedby"
```

- `rel="service-desc"` points to the OpenAPI spec
- `rel="describedby"` points to the JSON-LD context document

## Request ID

Every response includes a `Request-Id` header. Send your own via the `Request-Id` request header (max 64 chars, ASCII
printable); otherwise one is generated. The ID appears in error responses as `requestId` and in server logs.
