---
title: API Guide
description: REST endpoints, SSE streaming, query parameters, write operations, and error codes.
category: reference
tags: [ api, rest, sse, json-ld, openapi ]
---

# Cetacean API Guide

Observability and management API for Docker Swarm Mode clusters.

Cetacean runs as a single binary that connects to the Docker socket, caches swarm state in memory, and serves it over
HTTP. Read endpoints use GET; write operations use PUT, POST, PATCH, and DELETE gated
by [operations level](configuration.md#operations-level). Authentication is [pluggable](authentication.md) via
`auth.mode` (default: anonymous access).

The machine-readable OpenAPI spec is available at `/api` (JSON). For an interactive endpoint browser, see the [API Reference](api/explorer).

## Content Negotiation

Every resource URL serves JSON, HTML (the embedded SPA), or SSE depending on what the client asks for. No `/api/v1/`
prefix -- versioning lives in the media type.

### Resolution order

1. **File extension:** `.json`, `.html`, or `.atom` appended to any path (the highest priority)
2. **`Accept` header:** standard content negotiation
3. **Default:** `application/json` when `*/*` or no preference

### Supported types

| Accept value                       | Result                                     |
|------------------------------------|--------------------------------------------|
| `application/json`                 | JSON (latest version)                      |
| `application/vnd.cetacean.v1+json` | JSON pinned to v1                          |
| `text/html`                        | SPA                                        |
| `text/event-stream`                | SSE (only on endpoints that support it)    |
| `application/atom+xml`             | Atom feed (resource and history endpoints) |

All negotiated responses include `Vary: Accept`.

Requesting an unsupported type returns `406 Not Acceptable`.

```http tab
GET /services HTTP/1.1
Accept: application/json
```

```bash tab
curl -H "Accept: application/json" http://localhost:9000/services
```

Extensions also work — append `.json` or `.atom` to any resource path:

```http tab
GET /services.json HTTP/1.1
```

```bash tab
curl http://localhost:9000/services.json
```

## Atom Feeds

Resource list endpoints, resource detail endpoints, and the history, search, and recommendations endpoints all support
[Atom](https://www.rfc-editor.org/rfc/rfc4287) feeds. Request via `Accept: application/atom+xml` or append `.atom` to any supported path.

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

Endpoints that do not produce resource change data (write sub-resources, log streams, metrics, topology) return
`406 Not Acceptable`.

### Pagination

Atom feeds use cursor-based pagination. The feed includes a `next` link when more entries are available:

| Parameter | Description                                      |
|-----------|--------------------------------------------------|
| `before`  | Return entries older than this cursor ID         |
| `limit`   | Number of entries per page (default 50, max 200) |

```http tab
# Request an Atom feed
GET /services HTTP/1.1
Accept: application/atom+xml
###

# Request via extension
GET /services.atom HTTP/1.1
###

# Page through history feed
GET /history.atom?limit=50 HTTP/1.1

GET /history.atom?before=<cursor-id>&limit=50 HTTP/1.1
```

```bash tab
# Request an Atom feed
curl -H "Accept: application/atom+xml" http://localhost:9000/services

# Request via extension
curl http://localhost:9000/services.atom

# Page through history feed
curl "http://localhost:9000/history.atom?limit=50"
curl "http://localhost:9000/history.atom?before=<cursor-id>&limit=50"
```

### Caching

Atom feeds support ETags and conditional requests. Pass `If-None-Match` with a previous ETag to receive `304 Not
Modified` when the feed has not changed. Responses include `Vary: Accept, Authorization, Cookie` so caches
differentiate by format and user.

### Feed autodiscovery

JSON responses on feed-capable endpoints include a `Link: <...>; rel="alternate"; type="application/atom+xml"` header.
The SPA injects `<link rel="alternate" type="application/atom+xml">` in the HTML `<head>`, so feed readers that
support browser-based autodiscovery can find feeds automatically.

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

```http tab
# Paginate results
GET /services?limit=10&offset=20 HTTP/1.1
###

# Sort by field
GET /nodes?sort=hostname&dir=desc HTTP/1.1
###

# Search by name
GET /configs?search=nginx HTTP/1.1
###

# Filter with expression
GET /services?filter=name+contains+'web' HTTP/1.1
```

```bash tab
# Paginate results
curl "http://localhost:9000/services?limit=10&offset=20"

# Sort by field
curl "http://localhost:9000/nodes?sort=hostname&dir=desc"

# Search by name
curl "http://localhost:9000/configs?search=nginx"

# Filter with expression
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

```http tab
# Filter ready managers
GET /nodes?filter=role+%3D%3D+%22manager%22+%26%26+state+%3D%3D+%22ready%22 HTTP/1.1
###

# Filter failed tasks
GET /tasks?filter=state+%3D%3D+%22failed%22+%7C%7C+error+!%3D+%22%22 HTTP/1.1
###

# Filter stacks by service count
GET /stacks?filter=services+>+5 HTTP/1.1
```

```bash tab
# Filter ready managers
curl "http://localhost:9000/nodes?filter=role+%3D%3D+%22manager%22+%26%26+state+%3D%3D+%22ready%22"

# Filter failed tasks
curl "http://localhost:9000/tasks?filter=state+%3D%3D+%22failed%22+%7C%7C+error+!%3D+%22%22"

# Filter stacks by service count
curl "http://localhost:9000/stacks?filter=services+>+5"
```

## Response Format

All responses include [JSON-LD](https://json-ld.org/) annotations (`@context`, `@id`, `@type`) for self-description. Collection responses
wrap items in `{ items, total, limit, offset }` with [RFC 8288](https://www.rfc-editor.org/rfc/rfc8288) `Link` headers for pagination. Detail responses wrap
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

**`Prefer: return=minimal`:** Write endpoints honor [RFC 7240](https://www.rfc-editor.org/rfc/rfc7240) `Prefer: return=minimal`. When set, successful writes
return `204 No Content` instead of the updated resource. The response includes `Preference-Applied: return=minimal`.

Standard security headers (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Content-Security-Policy`)
are set on all responses. HSTS is added when TLS is enabled.

## Real-Time Events (SSE)

Every resource endpoint supports SSE in addition to JSON. Send `Accept: text/event-stream` to any list or detail URL to
open a per-resource event stream.

### Per-resource streams

List Endpoints stream events filtered by resource type. Detail Endpoints stream events for a single resource. Stack
streams include events for all member resources (services, tasks, configs, secrets, networks, volumes).

```http tab
# Stream all node events
GET /nodes HTTP/1.1
Accept: text/event-stream
###

# Stream events for a single service
GET /services/abc123 HTTP/1.1
Accept: text/event-stream
###

# Stream events for a stack and all its resources
GET /stacks/myapp HTTP/1.1
Accept: text/event-stream
```

```bash tab
# Stream all node events
curl -H "Accept: text/event-stream" http://localhost:9000/nodes

# Stream events for a single service
curl -H "Accept: text/event-stream" http://localhost:9000/services/abc123

# Stream events for a stack and all its resources
curl -H "Accept: text/event-stream" http://localhost:9000/stacks/myapp
```

This is the primary SSE mechanism -- the frontend uses per-resource streams for real-time updates on every page.

### Global event stream

`/events` provides a single stream of all resource changes:

```http tab
GET /events HTTP/1.1
Accept: text/event-stream
```

```bash tab
curl -H "Accept: text/event-stream" http://localhost:9000/events
```

### Event format

Single events are sent with the resource type as the event name:

```sse
id: 1
event: service
data: {"@id":"/services/abc","@type":"Service","type":"service","action":"update","id":"abc","resource":{...}}
```

When multiple events arrive within the batch interval (default 100ms), they are sent as a `batch` event:

```sse
id: 2
event: batch
data: [{"@id":"/services/abc","@type":"Service","type":"service","action":"update","id":"abc","resource":{...}},...]
```

### Filtering

Use `?types=` to subscribe to specific resource types:

```http tab
GET /events?types=service,node HTTP/1.1
Accept: text/event-stream
```

```bash tab
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

```http tab
GET /metrics?query=up&step=15&range=3600 HTTP/1.1
Accept: text/event-stream
```

```bash tab
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

## Endpoints

For the complete endpoint reference with request/response schemas and try-it-out, see the interactive [API Reference](api/explorer).

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
