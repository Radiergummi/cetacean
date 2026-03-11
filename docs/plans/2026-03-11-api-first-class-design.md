# First-Class API Design

## Goal

Make Cetacean's API a first-class, externally consumable API. Currently the API exists only to serve the embedded React SPA — this design makes it suitable for scripting, custom dashboards, and programmatic integration by any consumer.

## Standards

| Standard | Purpose |
|----------|---------|
| RFC 9457 | Error responses (`application/problem+json`) |
| JSON-LD (W3C) | Self-describing response shapes (`@context`, `@id`, `@type`) |
| RFC 8288 | `Link` header for pagination (next/prev) |
| RFC 8631 | `Link` header for API self-discovery (service-desc, describedby) |
| RFC 6585 | 429 Too Many Requests for connection limits |

## Content Negotiation

Same URLs serve humans and machines. No `/api/v1/` prefix — versioning lives in the media type.

### Resolution order

1. **File extension suffix** (`.json`, `.html`) — highest priority, overrides Accept header
2. **`Accept` header** — standard content negotiation
3. **Default** — `application/json` when `*/*` or no preference

### Versioning

- `Accept: application/json` → latest version (currently v1)
- `Accept: application/vnd.cetacean.v1+json` → pinned to v1
- No version = latest. Clients never have to send a version explicitly.

### Supported extensions

| Suffix | Content-Type |
|--------|-------------|
| `.json` | `application/json` |
| `.html` | `text/html` |
| (none) | negotiated via Accept header |

### Unsupported types

`406 Not Acceptable` when a client requests a content type an endpoint doesn't support (e.g. `text/event-stream` on `/nodes`).

`Vary: Accept` on all negotiated responses.

## URL Structure

### Resource endpoints (content-negotiated)

All support JSON and HTML (SPA). SSE where noted.

```
/nodes
/nodes/{id}
/nodes/{id}/tasks
/services
/services/{id}
/services/{id}/tasks
/services/{id}/logs          ← also supports text/event-stream
/tasks
/tasks/{id}
/tasks/{id}/logs             ← also supports text/event-stream
/configs
/configs/{id}
/secrets
/secrets/{id}
/networks
/networks/{id}
/volumes
/volumes/{name}
/stacks
/stacks/summary
/stacks/{name}
/search
/events                      ← text/event-stream only (406 for others)
/topology/networks
/topology/placement
/cluster
/cluster/metrics
/swarm
/plugins
/disk-usage
/history
/notifications/rules
```

### Meta endpoints (no content negotiation)

Plain JSON, no extensions, no versioning.

```
/-/health
/-/ready
/-/metrics/status
/-/metrics/query
/-/metrics/query_range
```

### API documentation

```
/api                         ← OpenAPI playground (HTML) or spec (JSON)
/api/context.jsonld          ← JSON-LD context document
```

## Router Logic

```
request comes in
  → strip extension suffix (.json/.html), set forced content type
  → match route
  → if forced content type OR Accept header indicates a type:
      → if endpoint supports that type → serve it
      → else → 406 Not Acceptable
  → else (*/*, no preference):
      → serve JSON
```

## Response Shapes

### Collections (list endpoints)

```json
{
  "@context": "/api/context.jsonld",
  "@type": "Collection",
  "items": [
    {
      "@id": "/services/def456",
      "@type": "Service",
      "ID": "def456",
      "Spec": { "..." : "..." }
    }
  ],
  "total": 42,
  "limit": 50,
  "offset": 0
}
```

Pagination via `Link` header (RFC 8288):
```
Link: </services?offset=50&limit=50>; rel="next", </services?offset=0&limit=50>; rel="prev"
```

### Detail endpoints

All detail endpoints return a consistent wrapper with `@id`, `@type`, the resource, and cross-references:

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

Cross-references use `@id` links throughout. Enriched tasks include linked service and node:

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

JSON-LD `@id` and `@type` appear only at the top level of each resource — nested Docker API objects (Spec, TaskTemplate, etc.) are passed through unchanged.

### Errors (RFC 9457)

Content-Type: `application/problem+json`. Compatible with JSON-LD via `@context`.

```json
{
  "@context": "/api/context.jsonld",
  "type": "about:blank",
  "title": "Not Found",
  "status": 404,
  "detail": "node abc123 not found",
  "instance": "/nodes/abc123"
}
```

Domain-specific problem types use `urn:cetacean:error:` namespace:

```json
{
  "@context": "/api/context.jsonld",
  "type": "urn:cetacean:error:filter-invalid",
  "title": "Invalid Filter Expression",
  "status": 400,
  "detail": "unexpected token at position 12",
  "instance": "/services",
  "requestId": "req-xxxxx",
  "expression": "name == "
}
```

RFC 9457 `type` is mapped as a predicate in the JSON-LD context (not aliased to `@type`) — the two concepts stay separate.

### SSE event payloads

SSE data payloads include `@id` and `@type`:

```
event: service
data: {"@id":"/services/abc","@type":"Service","action":"update","resource":{...}}
```

Connection limit errors (128 log streams, 256 SSE clients) return 429 with RFC 9457 body and `Retry-After` header.

## Caching

- **List/detail endpoints**: `ETag` header derived from resource version/mutation timestamp. Clients use `If-None-Match` → 304 Not Modified.
- **Static resources** (`/api`, `/api/context.jsonld`): `Cache-Control: public, max-age=3600`.
- **SSE/streaming**: No caching headers.
- **Metrics proxy** (`/-/metrics/*`): Pass through Prometheus response headers.

## Self-Discovery

Every response includes RFC 8631 `Link` headers:

```
Link: </api>; rel="service-desc", </api/context.jsonld>; rel="describedby"
```

## Documentation

- **`api/openapi.yaml`** — Hand-written OpenAPI spec, served at `/api` (JSON when `Accept: application/json`).
- **`/api` (HTML)** — OpenAPI playground UI (e.g. Scalar, Swagger UI) when `Accept: text/html`.
- **`/api/context.jsonld`** — JSON-LD context document, embedded in binary.
- **`docs/api.md`** — Hand-written markdown reference: auth guidance, content negotiation, query parameters, error handling, SSE, endpoint reference with curl examples.
- **`internal/api/openapi_test.go`** — Validation tests that load the OpenAPI spec and verify handler responses match schemas. Runs in `go test ./...`.

## Out of Scope

- Authentication/authorization (reverse proxy responsibility)
- General rate limiting (only existing connection limits, now with proper 429)
- Content types beyond JSON, HTML, and SSE (no CSV, protobuf, XML)
- HATEOAS beyond `@id` links (read-only API, nothing to act on)
- Client library generation (OpenAPI spec enables it, but not shipped)
- Multi-key sorting
