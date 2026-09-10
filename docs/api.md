---
title: API
description: REST endpoints, SSE streams, query parameters, write operations, and error codes.
category: guide
tags: [ api, rest, sse, json-ld, openapi ]
---

# API

Cetacean serves its cached view of the swarm over HTTP. Reads use `GET`; writes use `PUT`, `POST`, `PATCH`, and
`DELETE`, gated by [operations level][operations-level]. Authentication is
[pluggable][authentication] via [`auth.mode`][auth.mode] and defaults to anonymous access.

The OpenAPI spec is served as JSON at `GET /api`. Browsers get an interactive playground at the same path; the
hosted copy is the [API explorer][api-explorer].

## Content negotiation

Every resource URL serves JSON, HTML (the embedded [dashboard][dashboard]), SSE, or a feed format depending on what
the client asks for. There is no `/api/v1/` prefix; versioning lives in the media type.

### Resolution order

1. File extension appended to the path, which wins over everything else
2. `Accept` header, parsed per RFC 7231 with `q` values and wildcards
3. `application/json` when the client sends `*/*` or no `Accept` header

### Supported types

| `Accept` value                     | Extension   | Result                                       |
|------------------------------------|-------------|----------------------------------------------|
| `application/json`                 | `.json`     | JSON                                         |
| `application/vnd.cetacean.v1+json` |             | JSON, versioned alias of `application/json`  |
| `text/html`, `application/xhtml+xml` | `.html`   | The dashboard                                |
| `text/event-stream`                |             | SSE, on endpoints that support it            |
| `application/atom+xml`             | `.atom`     | Atom feed                                    |
| `application/feed+json`            | `.feed`     | JSON Feed 1.1                                |
| `application/vnd.jgf+json`         | `.jgf`      | JSON Graph Format, `/topology` only          |
| `application/graphml+xml`          | `.graphml`  | GraphML, `/topology` only                    |
| `text/vnd.graphviz`                | `.dot`      | Graphviz DOT, `/topology` only               |

All negotiated responses include `Vary: Accept`. Requesting a type an endpoint cannot produce returns
`406 Not Acceptable` with code [`API003`](api/errors#API003); asking for SSE on an endpoint without a stream
returns `406` with [`API001`](api/errors#API001).

```http tab
GET /services HTTP/1.1
Accept: application/json
```

```bash tab
curl -H "Accept: application/json" http://localhost:9000/services
```

## Feeds

Resource list and detail endpoints, plus `/events`, `/history`, `/search`, and
[`/recommendations`][recommendations], serve [Atom 1.0](https://www.rfc-editor.org/rfc/rfc4287) and [JSON Feed
1.1](https://www.jsonfeed.org/version/1.1/). A feed carries the resource's change history, not its current state.

### Supported endpoints

- `/nodes`, `/nodes/{id}`
- `/services`, `/services/{id}`
- `/tasks`, `/tasks/{id}`
- `/stacks`, `/stacks/{name}`
- `/configs`, `/configs/{id}`
- `/secrets`, `/secrets/{id}`
- `/networks`, `/networks/{id}`
- `/volumes`, `/volumes/{name}`
- `/events`, `/history`, `/search`, `/recommendations`

Endpoints that produce no resource change data (write sub-resources, log streams, metrics, topology) return `406`.

### Feed pagination

Feeds page by cursor. The feed carries a `next` link while more entries exist, per
[RFC 5005](https://www.rfc-editor.org/rfc/rfc5005).

| Parameter | Description                                      |
|-----------|--------------------------------------------------|
| `before`  | Return entries older than this cursor ID         |
| `limit`   | Entries per page (default 50, max 200)           |

```http tab
GET /services.atom HTTP/1.1

GET /history.atom?limit=50 HTTP/1.1

GET /history.atom?before=<cursor-id>&limit=50 HTTP/1.1
```

```bash tab
curl -H "Accept: application/atom+xml" http://localhost:9000/services
curl http://localhost:9000/services.atom
curl "http://localhost:9000/history.atom?limit=50"
curl "http://localhost:9000/history.atom?before=<cursor-id>&limit=50"
```

### Feed caching

Feeds carry an `ETag`. Pass `If-None-Match` with a previous value to get `304 Not Modified` when nothing changed.
Responses add `Vary: Authorization, Cookie` alongside `Vary: Accept` so caches separate formats and users.

### Feed autodiscovery

JSON responses on feed-capable endpoints carry a `Link` header with `rel="alternate"` for each feed type. The dashboard
injects an Atom `<link rel="alternate">` into the HTML `<head>` on resource, history, search, and recommendations
pages, so feed readers can find the feed from the page.

## Pagination

List endpoints page by query parameter or by HTTP `Range` header.

### Query parameters

| Parameter | Type   | Default | Description                                                    |
|-----------|--------|---------|----------------------------------------------------------------|
| `limit`   | int    | 50      | Items per page, capped at 200                                  |
| `offset`  | int    | 0       | Starting position                                              |
| `sort`    | string | none    | Sort field, varies by resource                                 |
| `dir`     | string | `asc`   | Sort direction: `asc` or `desc`                                |
| `search`  | string | none    | Case-insensitive substring match on name                       |
| `filter`  | string | none    | [expr-lang](https://expr-lang.org/) expression, max 512 chars  |

```http tab
GET /services?limit=10&offset=20 HTTP/1.1

GET /nodes?sort=hostname&dir=desc HTTP/1.1

GET /configs?search=nginx HTTP/1.1
```

```bash tab
curl "http://localhost:9000/services?limit=10&offset=20"
curl "http://localhost:9000/nodes?sort=hostname&dir=desc"
curl "http://localhost:9000/configs?search=nginx"
```

### Range header pagination

List endpoints accept `Range: items 0-24` and answer `206 Partial Content` with `Content-Range: items 0-24/142`.
Every list response sets `Accept-Ranges: items`. An offset past the end returns `416` with `Content-Range: items
*/142`. Query parameters take precedence when both are present.

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

Filter expressions use [expr-lang](https://expr-lang.org/) syntax and must evaluate to a boolean. Operators: `==`,
`!=`, `<`, `>`, `<=`, `>=`, `contains`, `startsWith`, `endsWith`, `in`, `not in`, `&&`, `||`, `!`.

| Resource | Fields                                                                                                                                                                            |
|----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Nodes    | `id`, `name` (hostname), `state` (`ready`/`down`/`unknown`), `role` (`manager`/`worker`), `availability` (`active`/`pause`/`drain`)                                                |
| Services | `id`, `name`, `image`, `mode` (`replicated`/`global`), `stack`                                                                                                                     |
| Tasks    | `id`, `state`, `desired_state`, `image`, `exit_code`, `error`, `service` (ID), `node` (ID), `slot` (int)                                                                            |
| Stacks   | `name`, `services`, `configs`, `secrets`, `networks`, `volumes` (all counts)                                                                                                       |
| Configs  | `id`, `name`                                                                                                                                                                       |
| Secrets  | `id`, `name`                                                                                                                                                                       |
| Networks | `id`, `name`, `driver`, `scope` (`swarm`/`local`)                                                                                                                                  |
| Volumes  | `name`, `driver`, `scope`                                                                                                                                                          |

Task `state` takes the Docker task states: `new`, `allocated`, `pending`, `assigned`, `accepted`, `preparing`,
`ready`, `starting`, `running`, `complete`, `shutdown`, `failed`, `rejected`, `remove`, `orphaned`. `exit_code` is
empty until the task reaches a terminal state.

```http tab
GET /nodes?filter=role+%3D%3D+%22manager%22+%26%26+state+%3D%3D+%22ready%22 HTTP/1.1

GET /tasks?filter=state+%3D%3D+%22failed%22+%7C%7C+error+!%3D+%22%22 HTTP/1.1

GET /stacks?filter=services+>+5 HTTP/1.1
```

```bash tab
curl "http://localhost:9000/nodes?filter=role+%3D%3D+%22manager%22+%26%26+state+%3D%3D+%22ready%22"
curl "http://localhost:9000/tasks?filter=state+%3D%3D+%22failed%22+%7C%7C+error+!%3D+%22%22"
curl "http://localhost:9000/stacks?filter=services+>+5"
```

## Response format

All JSON responses carry [JSON-LD](https://json-ld.org/) annotations (`@context`, `@id`, `@type`). Collection responses
wrap items as `{ items, total, limit, offset }` with [RFC 8288](https://www.rfc-editor.org/rfc/rfc8288) `Link` headers
for pagination. Detail responses wrap the resource with its cross-references, such as the services using a config, or
the service and node for a task.

## Errors

Errors follow [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details, with Content-Type
`application/problem+json` and `Cache-Control: no-store`.

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

Domain-specific errors carry a stable code as the last path segment of `type`:

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

Codes are a three-letter domain prefix plus a three-digit number. The
[error reference](api/errors) lists every code with its status, meaning and
resolution, grouped by domain.

Generic HTTP errors use `"type": "about:blank"`. `GET /api/errors` lists every code with its description and
suggestion; `GET /api/errors/{code}` returns one.

### Common errors

| Situation | Status | Codes | What to do |
|---|---|---|---|
| Resource changed between your read and your write | 409 | [`SVC001`](api/errors#SVC001), [`NOD002`](api/errors#NOD002), [`CFG005`](api/errors#CFG005), [`SEC005`](api/errors#SEC005) | Re-read the resource and retry |
| Endpoint above the configured [operations level][operations-level] | 403 | [`OPS001`](api/errors#OPS001) | Raise the operations level |
| [ACL][authorization] denies read or write | 403 | [`ACL001`](api/errors#ACL001), [`ACL002`](api/errors#ACL002) | The response names the resource and permission checked |
| `PATCH` sent with the wrong `Content-Type` | 415 | [`API004`](api/errors#API004) | Use `application/json-patch+json` or `application/merge-patch+json` |
| Docker daemon unreachable | 503 | [`ENG001`](api/errors#ENG001) | Check the socket and the daemon |

## Caching

JSON responses carry an `ETag` and `Cache-Control: no-cache`. Use `If-None-Match` for conditional requests:

```bash
curl -v http://localhost:9000/services
# < ETag: "3a7f..."

curl -H 'If-None-Match: "3a7f..."' http://localhost:9000/services
# < HTTP/1.1 304 Not Modified
```

Detail endpoints also set `Last-Modified` from the resource's update timestamp and honour `If-Modified-Since`. When
both conditional headers are present, `If-None-Match` wins.

`/api` and `/api/context.jsonld` return `Cache-Control: public, max-age=3600`; `/api/scalar.js` returns `max-age=86400`.
SSE and streaming endpoints set no caching headers.

## Response headers

`Allow` on `GET` and `HEAD` responses lists the methods available for that resource under the current
[operations level][operations-level] and [ACL][authorization] grants. Inspect it before attempting a
write.

`Accept-Patch` lists the patch formats a resource accepts, either `application/json-patch+json, application/merge-patch+json`
or `application/merge-patch+json` alone. It appears only when the operations level and ACL permit writes.

Write endpoints honour [RFC 7240](https://www.rfc-editor.org/rfc/rfc7240) `Prefer: return=minimal`, answering
`204 No Content` (or `201 Created` for a create) with `Preference-Applied: return=minimal` instead of the updated
resource.

Every response sets `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and `Content-Security-Policy`. HSTS
is added when TLS is enabled.

## Real-time events

Send `Accept: text/event-stream` to any resource list or detail URL to open a stream scoped to that path.

### Per-resource streams

A list stream carries events for one resource type. A detail stream carries events for one resource plus the resources
that belong to it: a node stream includes that node's tasks, a service stream includes that service's tasks, and a stack
stream includes its member services, tasks, configs, secrets, networks, and volumes.

```http tab
GET /nodes HTTP/1.1
Accept: text/event-stream

GET /services/abc123 HTTP/1.1
Accept: text/event-stream

GET /stacks/myapp HTTP/1.1
Accept: text/event-stream
```

```bash tab
curl -H "Accept: text/event-stream" http://localhost:9000/nodes
curl -H "Accept: text/event-stream" http://localhost:9000/services/abc123
curl -H "Accept: text/event-stream" http://localhost:9000/stacks/myapp
```

The dashboard uses per-resource streams for live updates on every page.

### Global event stream

`/events` carries every resource change in one stream:

```http tab
GET /events HTTP/1.1
Accept: text/event-stream
```

```bash tab
curl -H "Accept: text/event-stream" http://localhost:9000/events
```

### Event format

A single event uses the resource type as the event name:

```sse
id: 1
event: service
data: {"@id":"/services/abc","@type":"Service","type":"service","action":"update","id":"abc","resource":{...}}
```

The `action` field says what happened:

| Action | Meaning |
|---|---|
| `create` | The resource appeared. Clients tracking a collection's size should increment it. |
| `update` | An existing resource changed. The collection's size is unaffected. |
| `remove` | The resource is gone. |
| `ref_changed` | A resource this one cross-references changed. |
| `full_sync` | Sent as a `sync` event when the stream could not be replayed from the client's cursor. Refetch. |

Events arriving within the batch interval ([`server.sse.batch_interval`][server.sse.batch_interval], default 100ms)
are sent together as a `batch` event:

```sse
id: 2
event: batch
data: [{"@id":"/services/abc","@type":"Service","type":"service","action":"update","id":"abc","resource":{...}},...]
```

### Filtering

`?types=` on `/events` narrows the stream:

```http tab
GET /events?types=service,node HTTP/1.1
Accept: text/event-stream
```

```bash tab
curl -H "Accept: text/event-stream" "http://localhost:9000/events?types=service,node"
```

Valid types: `node`, `service`, `task`, `config`, `secret`, `network`, `volume`, `stack`. `sync` events always pass the
filter.

### Keepalive

The server writes an SSE comment line every 15 idle seconds so proxies and load balancers keep the connection open.
EventSource clients ignore it.

### Reconnection and replay

Each event carries an incrementing `id:`. EventSource clients send `Last-Event-ID` on reconnect and the server replays
what they missed from the change history. Detail and stack streams cannot be replayed, and a cursor older than the
history buffer cannot either; both cases send a `sync` event telling the client to refetch.

### Metrics streams

`GET /metrics` with `Accept: text/event-stream` pushes periodic updates instead of proxying one query to Prometheus.

```http tab
GET /metrics?query=up&step=15&range=3600 HTTP/1.1
Accept: text/event-stream
```

```bash tab
curl -H "Accept: text/event-stream" "http://localhost:9000/metrics?query=up&step=15&range=3600"
```

| Event     | Description                                                                  |
|-----------|------------------------------------------------------------------------------|
| `initial` | Full range query result on connect, same shape as Prometheus `query_range`. |
| `point`   | Single instant query result appended at each tick.                           |

Append `point` events to the data you already hold to build a rolling window.

## Connection limits

There is no general rate limiting. Concurrent streams are capped, and a request over the cap returns
`429 Too Many Requests` with `Retry-After: 5`.

| Stream                                                 | Limit | Code     |
|--------------------------------------------------------|-------|----------|
| SSE event clients (`/events` and per-resource streams) | 256   | `SSE001` |
| Log streams (`/services/{id}/logs`, `/tasks/{id}/logs`) | 128   | `LOG001` |
| Metrics streams (`/metrics` as SSE)                    | 64    | `MTR005` |

## Endpoints

`GET /api` serves the full OpenAPI spec with request and response schemas, and the
[API explorer][api-explorer] renders it interactively. The tables below are the shape of the surface.

### Reads

| Area | Endpoints |
|---|---|
| Nodes | `/nodes`, `/nodes/{id}`, `/nodes/{id}/tasks`, `/nodes/{id}/labels`, `/nodes/{id}/role` |
| Services | `/services`, `/services/{id}`, `/services/{id}/tasks`, `/services/{id}/logs` |
| Service spec sections | `/services/{id}/` + `env`, `labels`, `resources`, `healthcheck`, `placement`, `ports`, `update-policy`, `rollback-policy`, `log-driver`, `configs`, `secrets`, `networks`, `mounts`, `container-config`, `mode`, `endpoint-mode` |
| Tasks | `/tasks`, `/tasks/{id}`, `/tasks/{id}/logs` |
| Stacks | `/stacks`, `/stacks/summary`, `/stacks/{name}` |
| Configs and secrets | `/configs`, `/configs/{id}`, `/configs/{id}/labels`, `/secrets`, `/secrets/{id}`, `/secrets/{id}/labels` |
| Networks and volumes | `/networks`, `/networks/{id}`, `/volumes`, `/volumes/{name}` |
| Plugins | `/plugins`, `/plugins/{name}`, `/swarm/plugins` |
| Cluster | `/cluster`, `/cluster/metrics`, `/cluster/capacity`, `/swarm`, `/disk-usage` |
| Cross-resource | `/search?q=`, `/history`, `/events`, `/recommendations`, `/topology`, `/profile` |
| Metrics | `/metrics`, `/metrics/status`, `/metrics/labels`, `/metrics/labels/{name}` |
| Documentation | `/api`, `/api/context.jsonld`, `/api/errors`, `/api/errors/{code}` |
| Identity | `/auth/whoami` |
| Meta | `/-/health`, `/-/ready`, `/-/metrics`, `/-/licenses`, `/-/licenses/texts/{id}`, `/-/notices`, `/-/sbom.cdx`, `/-/docker-latest-version` |

`GET /search` takes `q` (required, max 200 characters) and `limit` (per type, default 3; `0` or a value above 1000
returns up to 1000). `POST /-/resync` forces a full re-fetch from the Docker socket.

### Writes

Each row gives the minimum [operations level][operations-level] the endpoint needs. Every write also
passes the per-resource [ACL][authorization] write check.

| Endpoint | Level |
|---|---|
| `PUT /services/{id}/scale` | 1 |
| `PUT /services/{id}/image` | 1 |
| `POST /services/{id}/rollback` | 1 |
| `POST /services/{id}/restart` | 1 |
| `PATCH /services/{id}/env` | 2 |
| `PATCH /services/{id}/labels` | 2 |
| `PATCH /services/{id}/resources` | 2 |
| `PUT`, `PATCH /services/{id}/healthcheck` | 2 |
| `PUT /services/{id}/placement` | 2 |
| `PATCH /services/{id}/ports` | 2 |
| `PATCH /services/{id}/update-policy` | 2 |
| `PATCH /services/{id}/rollback-policy` | 2 |
| `PATCH /services/{id}/log-driver` | 2 |
| `PATCH /services/{id}/configs` | 2 |
| `PATCH /services/{id}/secrets` | 2 |
| `PATCH /services/{id}/networks` | 2 |
| `PATCH /services/{id}/mounts` | 2 |
| `PATCH /services/{id}/container-config` | 2 |
| `PUT /services/{id}/mode` | 3 |
| `PUT /services/{id}/endpoint-mode` | 3 |
| `DELETE /services/{id}` | 3 |
| `PUT /nodes/{id}/availability` | 3 |
| `PUT /nodes/{id}/role` | 3 |
| `PATCH /nodes/{id}/labels` | 3 |
| `DELETE /nodes/{id}` | 3 |
| `DELETE /tasks/{id}` | 3 |
| `DELETE /stacks/{name}` | 3 |
| `POST /configs` | 2 |
| `PATCH /configs/{id}/labels` | 2 |
| `DELETE /configs/{id}` | 3 |
| `POST /secrets` | 2 |
| `PATCH /secrets/{id}/labels` | 2 |
| `DELETE /secrets/{id}` | 3 |
| `DELETE /networks/{id}` | 3 |
| `DELETE /volumes/{name}` | 3 |
| `PATCH /swarm/orchestration` | 2 |
| `PATCH /swarm/raft` | 2 |
| `PATCH /swarm/dispatcher` | 2 |
| `PATCH /swarm/ca` | 3 |
| `PATCH /swarm/encryption` | 3 |
| `POST /swarm/rotate-token` | 3 |
| `POST /swarm/rotate-unlock-key` | 3 |
| `POST /swarm/force-rotate-ca` | 3 |
| `GET /swarm/unlock-key` | 3 |
| `POST /swarm/unlock` | 3 |
| `POST /plugins/{name}/enable` | 2 |
| `POST /plugins/{name}/disable` | 2 |
| `PATCH /plugins/{name}/settings` | 2 |
| `POST /plugins` | 3 |
| `POST /plugins/privileges` | 3 |
| `POST /plugins/{name}/upgrade` | 3 |
| `DELETE /plugins/{name}` | 3 |

> [!NOTE]
> `GET /swarm/unlock-key` returns a credential, so it is gated at level 3 like the writes beside it.

### Preconditions

Every write endpoint whose exact path also serves a `GET` accepts an optional `If-Match` request
header ([RFC 9110 §13.1.1](https://www.rfc-editor.org/rfc/rfc9110#section-13.1.1)). Supply the
`ETag` a `GET` on that same path returned; if the resource has changed since, the write is
refused with `412 Precondition Failed` (error code `API013`) instead of being applied. The header
is always optional — omit it and the write proceeds exactly as it did before this existed.

29 endpoints support it: `PATCH /services/{id}/env`, `PATCH /services/{id}/labels`,
`PATCH /services/{id}/resources`, `PUT`/`PATCH /services/{id}/healthcheck`,
`PUT /services/{id}/placement`, `PATCH /services/{id}/ports`,
`PATCH /services/{id}/update-policy`, `PATCH /services/{id}/rollback-policy`,
`PATCH /services/{id}/log-driver`, `PATCH /services/{id}/configs`,
`PATCH /services/{id}/secrets`, `PATCH /services/{id}/networks`,
`PATCH /services/{id}/mounts`, `PATCH /services/{id}/container-config`,
`PUT /services/{id}/mode`, `PUT /services/{id}/endpoint-mode`, `DELETE /services/{id}`,
`PATCH /nodes/{id}/labels`, `PUT /nodes/{id}/role`, `DELETE /nodes/{id}`,
`PATCH /configs/{id}/labels`, `DELETE /configs/{id}`, `PATCH /secrets/{id}/labels`,
`DELETE /secrets/{id}`, `DELETE /networks/{id}`, `DELETE /volumes/{name}`,
`DELETE /tasks/{id}`, `DELETE /stacks/{name}`, `DELETE /plugins/{name}`.

23 do not. Most of these are action-style endpoints with no `GET` at that exact path to compare
an `ETag` against: `PUT /services/{id}/scale`, `PUT /services/{id}/image`,
`POST /services/{id}/restart`, `POST /services/{id}/rollback`, `PUT /nodes/{id}/availability`,
`POST /plugins/{name}/enable`, `POST /plugins/{name}/disable`, `POST /plugins/{name}/upgrade`,
`PATCH /plugins/{name}/settings`, `POST /plugins/privileges`, `PATCH /swarm/ca`,
`PATCH /swarm/dispatcher`, `PATCH /swarm/encryption`, `PATCH /swarm/orchestration`,
`PATCH /swarm/raft`, `POST /swarm/rotate-token`, `POST /swarm/rotate-unlock-key`,
`POST /swarm/force-rotate-ca`, `POST /swarm/unlock`, and `POST /auth/logout`. The remaining
three — `POST /configs`, `POST /secrets`, `POST /plugins` — are deliberately excluded for a
different reason: their nearest `GET` is the collection listing, and its `ETag` turns over on any
member change, which would make "create only if the collection is unchanged" a precondition
almost nothing could ever satisfy.

## MCP server

Cetacean can also serve its cluster view over the Model Context Protocol. Set [`mcp.enabled`][mcp.enabled] to
`true` and the server mounts at `/mcp`. See [MCP Server][mcp] for transport, authorization, and prompts, and [MCP
tools and resources][mcp-tools] for the catalog.

## Self-discovery

Every response outside the `/-/` meta endpoints carries [RFC 8631](https://www.rfc-editor.org/rfc/rfc8631) `Link`
headers:

```http
Link: </api>; rel="service-desc", </api/context.jsonld>; rel="describedby"
```

`service-desc` points at the OpenAPI spec, `describedby` at the JSON-LD context document.

## Request ID

Every response carries a `Request-Id` header. Send your own in the `Request-Id` request header (max 64 printable ASCII
characters) or the server generates one. The value appears in error responses as `requestId` and in the server logs.

[api-explorer]: api/explorer
[auth.mode]: configuration#auth.mode
[authentication]: authentication
[authorization]: authorization
[dashboard]: dashboard
[mcp-tools]: mcp-tools
[mcp.enabled]: configuration#mcp.enabled
[mcp]: mcp
[operations-level]: configuration#operations-level
[recommendations]: recommendations
[server.sse.batch_interval]: configuration#server.sse.batch_interval
