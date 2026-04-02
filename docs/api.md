---
title: API Reference
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

All list endpoints also accept the HTTP `Range` header with the `items` unit. This follows the pattern used by
[Heroku](https://devcenter.heroku.com/articles/platform-api-reference#ranges) and other APIs for cursor-free
pagination.

```bash
# First 25 items
curl -H "Range: items 0-24" http://localhost:9000/services

# Items 50-74
curl -H "Range: items 50-74" http://localhost:9000/services
```

The response uses standard HTTP range semantics:

| Status                      | Meaning                                                                                              |
|-----------------------------|------------------------------------------------------------------------------------------------------|
| `200 OK`                    | The full collection fits within the requested range (or no Range header).                            |
| `206 Partial Content`       | A subset was returned. Check `Content-Range` for position.                                           |
| `416 Range Not Satisfiable` | The requested offset is beyond the total. `Content-Range: items */TOTAL` tells you the actual count. |

Partial responses include a `Content-Range` header:

```
Content-Range: items 0-24/142
```

All list responses include `Accept-Ranges: items` to advertise support.

When both query parameters (`limit`/`offset`) and a `Range` header are present, query parameters take precedence.
Multipart ranges (e.g., `items 0-9, 50-59`) are not supported and return `416`.

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

All responses use JSON-LD annotations (`@context`, `@id`, `@type`) for self-description.

### Collections

```json
{
  "@context": "/api/context.jsonld",
  "@type": "Collection",
  "items": [
    {
      "ID": "abc123",
      "Spec": {
        "...": "..."
      }
    }
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
  "node": {
    "...": "..."
  },
  "services": [
    {
      "@id": "/services/def456",
      "name": "web"
    }
  ]
}
```

Detail responses for configs, secrets, networks, and volumes include a `services` array of cross-references to services
that use the resource.

Task details include linked service and node references:

```json
{
  "@context": "/api/context.jsonld",
  "@id": "/tasks/abc123",
  "@type": "Task",
  "task": {
    "...": "..."
  },
  "service": {
    "@id": "/services/def456",
    "name": "web"
  },
  "node": {
    "@id": "/nodes/ghi789",
    "hostname": "worker-1"
  }
}
```

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

### Error code reference

#### API — Protocol and content negotiation

| Code   | Status | Title                        | Description                                                                                  | Suggestion                                                                         |
|--------|--------|------------------------------|----------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------|
| API001 | 406    | SSE Not Supported            | This endpoint does not support Server-Sent Events.                                           | Use `Accept: application/json` instead of `text/event-stream`.                     |
| API002 | 406    | SSE Required                 | This endpoint only supports Server-Sent Events.                                              | Use `Accept: text/event-stream`.                                                   |
| API003 | 406    | Not Acceptable               | The `Accept` header does not match any media type this endpoint can produce.                 | Use `Accept: application/json`, `text/event-stream`, or `text/html`.               |
| API004 | 415    | Invalid Patch Content-Type   | The `Content-Type` header does not match a supported patch format.                           | Use `Content-Type: application/merge-patch+json` or `application/json-patch+json`. |
| API005 | 500    | Streaming Not Supported      | The server's response writer does not support streaming (no `http.Flusher`).                 | Server configuration issue. Check that no middleware is buffering responses.       |
| API006 | 400    | Invalid Request Body         | The request body could not be decoded as valid JSON.                                         | Ensure the request body is well-formed JSON matching the expected schema.          |
| API007 | 400    | Unreadable Request Body      | The request body could not be read.                                                          | Ensure the request includes a body and `Content-Length` is correct.                |
| API008 | 400    | Invalid JSON                 | The request body is not valid JSON.                                                          | Check for syntax errors in the JSON payload.                                       |
| API009 | 500    | Internal Serialization Error | The server failed to serialize or deserialize internal state.                                | Server bug. Check the Cetacean logs.                                               |
| API010 | 409    | Patch Test Failed            | A JSON Patch `test` operation failed — the resource state does not match the expected value. | Reload the resource and retry the patch with updated test values.                  |
| API011 | 400    | Patch Application Failed     | The JSON Patch could not be applied to the resource.                                         | Check the patch operations for correctness.                                        |

#### AUT — Authentication

| Code   | Status | Title                         | Description                                                          | Suggestion                                                 |
|--------|--------|-------------------------------|----------------------------------------------------------------------|------------------------------------------------------------|
| AUT001 | 401    | Not Authenticated             | No valid credentials were provided.                                  | Log in or provide a valid authentication token.            |
| AUT002 | 403    | Authorization Denied          | The identity provider denied authorization.                          | Check your account permissions with the identity provider. |
| AUT003 | 400    | Authentication Callback Error | The authentication callback contained invalid or missing parameters. | Retry the login flow from the beginning.                   |
| AUT004 | 500    | Authentication Server Error   | An internal error occurred during authentication.                    | Retry the login flow. If it persists, check server logs.   |

#### ACL — Authorization (RBAC)

| Code   | Status | Title               | Description                                         | Suggestion                                                                |
|--------|--------|---------------------|-----------------------------------------------------|---------------------------------------------------------------------------|
| ACL001 | 403    | Access Denied       | You do not have permission to access this resource. | Check your [ACL policy grants](authorization.md).                         |
| ACL002 | 403    | Write Access Denied | You do not have write permission on this resource.  | Check your [ACL policy grants](authorization.md) for `write` permissions. |

#### OPS — Operations level

| Code   | Status | Title                    | Description                                                                                                              | Suggestion                                      |
|--------|--------|--------------------------|--------------------------------------------------------------------------------------------------------------------------|-------------------------------------------------|
| OPS001 | 403    | Operations Level Too Low | The operation requires a higher [operations level](configuration.md#operations-level) than the server is configured for. | Increase `server.operations_level` and restart. |

#### FLT — Filter expressions

| Code   | Status | Title                      | Description                                                  | Suggestion                                                                                            |
|--------|--------|----------------------------|--------------------------------------------------------------|-------------------------------------------------------------------------------------------------------|
| FLT001 | 400    | Filter Expression Too Long | The filter expression exceeds the maximum allowed length.    | Shorten the filter expression.                                                                        |
| FLT002 | 400    | Invalid Filter Expression  | The filter expression could not be compiled.                 | Check the expression syntax. Filters use the [expr-lang](https://expr-lang.org/) expression language. |
| FLT003 | 400    | Filter Evaluation Error    | The filter expression compiled but failed during evaluation. | Check that the expression references valid fields for this resource type.                             |

#### SEA — Search

| Code   | Status | Title                 | Description                                                            | Suggestion                                |
|--------|--------|-----------------------|------------------------------------------------------------------------|-------------------------------------------|
| SEA001 | 400    | Missing Search Query  | The required query parameter `q` is missing.                           | Provide a search query: `/search?q=term`. |
| SEA002 | 400    | Search Query Too Long | The search query exceeds the maximum allowed length of 200 characters. | Shorten the search query.                 |

#### MTR — Metrics / Prometheus

| Code   | Status | Title                     | Description                                                                   | Suggestion                                                            |
|--------|--------|---------------------------|-------------------------------------------------------------------------------|-----------------------------------------------------------------------|
| MTR001 | 503    | Prometheus Not Configured | No Prometheus URL is configured.                                              | Set `prometheus.url` and restart.                                     |
| MTR002 | 502    | Prometheus Unreachable    | The configured Prometheus server is not responding.                           | Check that Prometheus is running and reachable at the configured URL. |
| MTR003 | 400    | Missing Metrics Query     | The required query parameter is missing.                                      | Provide a PromQL query parameter.                                     |
| MTR004 | 400    | Invalid Metrics Step      | The step parameter is outside the allowed range.                              | Use a step value between 5 and 300 seconds.                           |
| MTR005 | 429    | Too Many Metrics Streams  | The maximum number of concurrent metrics stream connections has been reached. | Close an existing metrics stream connection before opening a new one. |
| MTR006 | 400    | Missing Label Name        | The label name path parameter is missing.                                     | Provide a label name in the URL path.                                 |
| MTR007 | 500    | Prometheus Request Failed | Failed to create the request to the Prometheus server.                        | Server-side error. Check the Cetacean logs.                           |

#### LOG — Log streaming

| Code   | Status | Title                        | Description                                                                              | Suggestion                                                                      |
|--------|--------|------------------------------|------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------|
| LOG001 | 429    | Too Many Log Streams         | The maximum number of concurrent log stream connections has been reached.                | Close an existing log stream before opening a new one.                          |
| LOG002 | 400    | Invalid Stream Parameter     | The `stream` parameter must be either `stdout` or `stderr`.                              | Use `stream=stdout` or `stream=stderr`.                                         |
| LOG003 | 400    | Invalid After Parameter      | The `after` parameter must be an RFC 3339 timestamp or a Go duration string.             | Use a format like `2024-01-01T00:00:00Z` or `1h30m`.                            |
| LOG004 | 400    | Invalid Before Parameter     | The `before` parameter must be an RFC 3339 timestamp or a Go duration string.            | Use a format like `2024-01-01T00:00:00Z` or `1h30m`.                            |
| LOG005 | 400    | Before Not Supported For SSE | The `before` parameter is not supported for SSE log streams because they are open-ended. | Remove the `before` parameter when using SSE, or use a JSON request instead.    |
| LOG006 | 500    | Log Retrieval Failed         | Failed to retrieve logs from the Docker Engine.                                          | Check that the service or task still exists and the Docker Engine is reachable. |
| LOG007 | 500    | Log Parse Failed             | Logs were retrieved but could not be parsed.                                             | Server-side error. Check the Cetacean logs.                                     |
| LOG008 | 500    | Log Stream Failed            | Failed to open the log stream from the Docker Engine.                                    | Check that the service or task still exists and the Docker Engine is reachable. |

#### SSE — SSE connections

| Code   | Status | Title                    | Description                                                        | Suggestion                                                 |
|--------|--------|--------------------------|--------------------------------------------------------------------|------------------------------------------------------------|
| SSE001 | 429    | Too Many SSE Connections | The maximum number of concurrent SSE connections has been reached. | Close an existing SSE connection before opening a new one. |

#### ENG — Docker Engine

| Code   | Status | Title                       | Description                                                                                                   | Suggestion                                                                                 |
|--------|--------|-----------------------------|---------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------|
| ENG001 | 503    | Docker Engine Unavailable   | The Docker Engine is not responding. The daemon may be stopped, restarting, or the socket may be unreachable. | Check that the Docker daemon is running and that Cetacean has access to the Docker socket. |
| ENG002 | 503    | Docker Version Check Failed | Could not determine the latest Docker Engine version from the GitHub API.                                     | Transient network error. Try again later.                                                  |
| ENG003 | 400    | Docker Validation Error     | The Docker Engine rejected the request due to invalid arguments.                                              | Check the request parameters for correctness.                                              |
| ENG004 | 500    | Docker Engine Error         | An unexpected error occurred while communicating with the Docker Engine.                                      | Check the Cetacean and Docker daemon logs.                                                 |

#### SWM — Swarm operations

| Code   | Status | Title                       | Description                                                                            | Suggestion                                                                        |
|--------|--------|-----------------------------|----------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------|
| SWM001 | 501    | Swarm API Not Available     | This node may not be a swarm manager, or the Docker Engine may not support swarm mode. | Ensure Cetacean is connected to a swarm manager node.                             |
| SWM002 | 503    | Swarm Inspect Failed        | Failed to inspect the current swarm state.                                             | Check that the swarm is healthy and retry.                                        |
| SWM003 | 500    | Swarm Update Failed         | The swarm configuration update failed.                                                 | Check the Cetacean and Docker daemon logs.                                        |
| SWM004 | 501    | Disk Usage Not Available    | Disk usage information is not available from the Docker Engine.                        | Ensure Cetacean is connected to a Docker Engine that supports the disk usage API. |
| SWM005 | 500    | Disk Usage Failed           | Failed to retrieve disk usage information.                                             | Check the Docker daemon logs.                                                     |
| SWM006 | 500    | Token Rotation Failed       | Failed to rotate the swarm join token.                                                 | Check the Docker daemon logs.                                                     |
| SWM007 | 500    | Unlock Key Rotation Failed  | Failed to rotate the swarm unlock key.                                                 | Check the Docker daemon logs.                                                     |
| SWM008 | 500    | Swarm Unlock Failed         | Failed to unlock the swarm with the provided key.                                      | Verify the unlock key is correct and try again.                                   |
| SWM009 | 500    | Unlock Key Retrieval Failed | Failed to retrieve the swarm unlock key.                                               | Check the Docker daemon logs.                                                     |
| SWM010 | 400    | Unlock Key Required         | The unlock key is required to unlock the swarm.                                        | Provide the `unlockKey` field in the request body.                                |
| SWM011 | 400    | Invalid Token Target        | The token rotation target must be either `worker` or `manager`.                        | Use `target=worker` or `target=manager`.                                          |

#### NOD — Node operations

| Code   | Status | Title                      | Description                                                        | Suggestion                                                                       |
|--------|--------|----------------------------|--------------------------------------------------------------------|----------------------------------------------------------------------------------|
| NOD001 | 409    | Node Not Down              | The node cannot be removed because it is not in the down state.    | Drain the node first, wait for it to reach the down state, or use force removal. |
| NOD002 | 409    | Node Version Conflict      | The node was modified by another client between read and write.    | Reload the node and retry.                                                       |
| NOD003 | 404    | Node Not Found             | The specified node does not exist in the swarm.                    | Check the node ID or hostname.                                                   |
| NOD004 | 400    | Invalid Availability Value | The availability value must be one of: `active`, `drain`, `pause`. | Use `availability=active`, `availability=drain`, or `availability=pause`.        |
| NOD005 | 400    | Invalid Role Value         | The role value must be one of: `worker`, `manager`.                | Use `role=worker` or `role=manager`.                                             |

#### SVC — Service operations

| Code   | Status | Title                                 | Description                                                               | Suggestion                                              |
|--------|--------|---------------------------------------|---------------------------------------------------------------------------|---------------------------------------------------------|
| SVC001 | 409    | Service Version Conflict              | The service was modified by another client between read and write.        | Reload the service and retry.                           |
| SVC002 | 409    | Service In Use                        | The service cannot be removed because it is managed by a stack.           | Remove the stack that manages this service first.       |
| SVC003 | 404    | Service Not Found                     | The specified service does not exist in the swarm.                        | Check the service ID or name.                           |
| SVC004 | 400    | Replicas Required                     | The `replicas` field is required for this operation.                      | Provide the `replicas` field in the request body.       |
| SVC005 | 400    | Cannot Scale Global Service           | Global-mode services run one task per node and cannot be scaled manually. | Switch the service to replicated mode first.            |
| SVC006 | 400    | Image Required                        | The `image` field is required for image updates.                          | Provide the `image` field in the request body.          |
| SVC007 | 400    | No Previous Spec                      | The service has no previous specification to rollback to.                 | Rollback is only available after at least one update.   |
| SVC008 | 400    | Invalid Service Mode                  | The service mode must be one of: `replicated`, `global`.                  | Use `mode=replicated` or `mode=global`.                 |
| SVC009 | 400    | Replicas Required For Replicated Mode | When switching to replicated mode, the `replicas` field is required.      | Provide the `replicas` field alongside the mode change. |
| SVC010 | 400    | Invalid Endpoint Mode                 | The endpoint mode must be one of: `vip`, `dnsrr`.                         | Use `mode=vip` or `mode=dnsrr`.                         |
| SVC011 | 400    | Invalid Resource Specification        | The merged resource specification is not valid.                           | Check the resource limits and reservations.             |
| SVC012 | 400    | Invalid Update Policy                 | The merged update policy specification is not valid.                      | Check the update policy fields.                         |
| SVC013 | 400    | Invalid Rollback Policy               | The merged rollback policy specification is not valid.                    | Check the rollback policy fields.                       |
| SVC014 | 400    | Invalid Healthcheck                   | The merged healthcheck specification is not valid.                        | Check the healthcheck fields.                           |
| SVC015 | 400    | Config Missing Required Fields        | Each config reference must include `configID` and `configName`.           | Provide both fields for every config entry.             |
| SVC016 | 400    | Secret Missing Required Fields        | Each secret reference must include `secretID` and `secretName`.           | Provide both fields for every secret entry.             |
| SVC017 | 400    | Network Missing Target                | Each network attachment must include a target network ID.                 | Provide the `target` field for every network entry.     |
| SVC018 | 400    | Invalid Patch Result                  | The JSON patch produced an invalid result.                                | Check the patch operations for correctness.             |
| SVC019 | 400    | Invalid Log Driver Specification      | The merged log driver specification is not valid.                         | Check the log driver name and options.                  |

#### TSK — Task operations

| Code   | Status | Title                | Description                                                       | Suggestion                                           |
|--------|--------|----------------------|-------------------------------------------------------------------|------------------------------------------------------|
| TSK001 | 409    | Task Already Removed | The task could not be removed because Docker no longer tracks it. | The task may have been cleaned up. Refresh the page. |
| TSK002 | 404    | Task Not Found       | The specified task does not exist.                                | Tasks are ephemeral and may have been cleaned up.    |

#### STK — Stack operations

| Code   | Status | Title           | Description                                                                                                             | Suggestion            |
|--------|--------|-----------------|-------------------------------------------------------------------------------------------------------------------------|-----------------------|
| STK001 | 404    | Stack Not Found | The specified stack does not exist. Stacks are derived from service labels and disappear when all services are removed. | Check the stack name. |

#### VOL — Volume operations

| Code   | Status | Title            | Description                                                                   | Suggestion                                                                   |
|--------|--------|------------------|-------------------------------------------------------------------------------|------------------------------------------------------------------------------|
| VOL001 | 409    | Volume In Use    | The volume cannot be removed because it is mounted by one or more containers. | Stop or remove the containers using this volume first, or use force removal. |
| VOL002 | 404    | Volume Not Found | The specified volume does not exist.                                          | Check the volume name.                                                       |

#### NET — Network operations

| Code   | Status | Title                        | Description                                                                                        | Suggestion                                                        |
|--------|--------|------------------------------|----------------------------------------------------------------------------------------------------|-------------------------------------------------------------------|
| NET001 | 409    | Network Has Active Endpoints | The network cannot be removed because it has active endpoints from running containers or services. | Disconnect or remove the services attached to this network first. |
| NET002 | 404    | Network Not Found            | The specified network does not exist.                                                              | Check the network ID.                                             |

#### CFG — Config operations

| Code   | Status | Title                   | Description                                                                    | Suggestion                                              |
|--------|--------|-------------------------|--------------------------------------------------------------------------------|---------------------------------------------------------|
| CFG001 | 409    | Config In Use           | The config cannot be removed because it is referenced by one or more services. | Remove the config reference from all services first.    |
| CFG002 | 404    | Config Not Found        | The specified config does not exist.                                           | Check the config ID.                                    |
| CFG003 | 409    | Config Name Conflict    | A config with this name already exists.                                        | Choose a different name or remove the existing config.  |
| CFG004 | 400    | Invalid Config          | The config creation request is invalid.                                        | Provide a non-empty name and valid base64-encoded data. |
| CFG005 | 409    | Config Version Conflict | The config was modified concurrently.                                          | Retry with the latest version.                          |

#### SEC — Secret operations

| Code   | Status | Title                   | Description                                                                    | Suggestion                                              |
|--------|--------|-------------------------|--------------------------------------------------------------------------------|---------------------------------------------------------|
| SEC001 | 409    | Secret In Use           | The secret cannot be removed because it is referenced by one or more services. | Remove the secret reference from all services first.    |
| SEC002 | 404    | Secret Not Found        | The specified secret does not exist.                                           | Check the secret ID.                                    |
| SEC003 | 409    | Secret Name Conflict    | A secret with this name already exists.                                        | Choose a different name or remove the existing secret.  |
| SEC004 | 400    | Invalid Secret          | The secret creation request is invalid.                                        | Provide a non-empty name and valid base64-encoded data. |
| SEC005 | 409    | Secret Version Conflict | The secret was modified concurrently.                                          | Retry with the latest version.                          |

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

**Security headers:** All responses include:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`
- `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https:`
- `Strict-Transport-Security: max-age=63072000; includeSubDomains` (when TLS is enabled)

## Real-Time Events (SSE)

Every resource endpoint supports SSE in addition to JSON. Send `Accept: text/event-stream` to any list or detail URL to
open a per-resource event stream.

### Per-resource streams

List endpoints stream events filtered by resource type. Detail endpoints stream events for a single resource. Stack
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

```bash
curl http://localhost:9000/-/health
# {"status":"ok","version":"...","commit":"...","buildDate":"..."}

curl http://localhost:9000/-/ready
# {"status":"ready"}  (or 503 {"status":"not_ready"})
```

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

```bash
curl http://localhost:9000/cluster
curl http://localhost:9000/cluster/metrics
```

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

```bash
curl http://localhost:9000/nodes
curl http://localhost:9000/nodes/abc123
curl http://localhost:9000/nodes/abc123/tasks
```

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

```bash
curl http://localhost:9000/tasks
curl http://localhost:9000/tasks/abc123
curl -H "Accept: text/event-stream" http://localhost:9000/tasks/abc123/logs
```

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

```bash
curl http://localhost:9000/stacks
curl http://localhost:9000/stacks/summary
curl http://localhost:9000/stacks/myapp
```

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

```bash
curl http://localhost:9000/configs
curl http://localhost:9000/configs/abc123

# Create a config (data is base64-encoded)
curl -X POST -d '{"name": "my-config", "data": "aGVsbG8="}' http://localhost:9000/configs
```

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

```bash
curl http://localhost:9000/secrets
curl http://localhost:9000/secrets/abc123

# Create a secret (data is base64-encoded)
curl -X POST -d '{"name": "my-secret", "data": "c3VwZXJzZWNyZXQ="}' http://localhost:9000/secrets
```

### Networks

| Method | Path             | Description                                    | Parameters                                           |
|--------|------------------|------------------------------------------------|------------------------------------------------------|
| GET    | `/networks`      | List networks.                                 | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/networks/{id}` | Network detail with cross-referenced services. | --                                                   |
| DELETE | `/networks/{id}` | Remove a network. Tier 3.                      | --                                                   |

```bash
curl http://localhost:9000/networks
curl http://localhost:9000/networks/abc123
```

### Volumes

Volumes are keyed by name, not ID.

| Method | Path              | Description                                   | Parameters                                           |
|--------|-------------------|-----------------------------------------------|------------------------------------------------------|
| GET    | `/volumes`        | List volumes.                                 | `search`, `filter`, `sort`, `dir`, `limit`, `offset` |
| GET    | `/volumes/{name}` | Volume detail with cross-referenced services. | --                                                   |
| DELETE | `/volumes/{name}` | Remove a volume. Tier 3.                      | --                                                   |

```bash
curl http://localhost:9000/volumes
curl http://localhost:9000/volumes/my-data
```

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

```bash
curl http://localhost:9000/topology/networks
curl http://localhost:9000/topology/placement
```

### Recommendations

| Method | Path               | Description                                                                               |
|--------|--------------------|-------------------------------------------------------------------------------------------|
| GET    | `/recommendations` | All active cluster health recommendations, sorted by severity. Includes severity summary. |

Returns a JSON-LD `RecommendationCollection` with `items` (array of recommendations), `total`, `summary` (severity
counts), and `computedAt`. Four domains: resource sizing, config hygiene (missing health checks, restart policies),
operational (flaky services, disk/memory pressure), and cluster topology (single replicas, manager workloads, uneven
distribution).

```bash
curl http://localhost:9000/recommendations
```

### Profile

| Method | Path       | Description                                           |
|--------|------------|-------------------------------------------------------|
| GET    | `/profile` | Current user profile (content-negotiated, with ETag). |

Unlike `/auth/whoami`, this endpoint participates in content negotiation and includes ETag support.

```bash
curl http://localhost:9000/profile
```

### Events

| Method | Path      | Description                                 | Parameters                |
|--------|-----------|---------------------------------------------|---------------------------|
| GET    | `/events` | SSE-only. Real-time resource change stream. | `types` (comma-separated) |

Returns `406` for non-SSE requests. See [Real-Time Events](#real-time-events-sse) for details.

```bash
curl -H "Accept: text/event-stream" http://localhost:9000/events
curl -H "Accept: text/event-stream" "http://localhost:9000/events?types=service,task"
```

### Authentication

See [Authentication](authentication.md) for full details on each auth mode.

| Method | Path             | Description                                                        |
|--------|------------------|--------------------------------------------------------------------|
| GET    | `/auth/whoami`   | Current identity. Returns `Cache-Control: no-store`.               |
| GET    | `/auth/login`    | Initiate OIDC login flow (OIDC mode only).                         |
| GET    | `/auth/callback` | OIDC callback (OIDC mode only; redirected by IdP).                 |
| POST   | `/auth/logout`   | Clear session, optionally redirect to IdP logout (OIDC mode only). |

```bash
curl http://localhost:9000/auth/whoami
# {"subject":"anonymous","displayName":"Anonymous","provider":"none"}
```

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

Every request gets an `X-Request-ID` header in the response. You can send your own via the `X-Request-ID` request
header (max 64 chars, ASCII printable); otherwise one is generated automatically. The request ID appears in error
responses as `requestId` and in server logs.
