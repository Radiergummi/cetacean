---
title: API
description: REST endpoints, SSE streams, query parameters, write operations, and error codes.
category: guide
tags: [api, rest, sse, json-ld, openapi]
---

# API

Cetacean serves its cached view of the swarm over HTTP. Reads use `GET`; writes use `PUT`, `POST`, `PATCH`, and
`DELETE`, gated by [operations level][operations-level]. Authentication is
[pluggable][authentication] via [`auth.mode`][auth.mode] and defaults to anonymous access.

The OpenAPI spec is served as JSON at `GET /api` and as YAML at `GET /api/openapi.yaml`. Browsers get an
interactive playground at `/api`; the hosted copy is the [API explorer][api-explorer]. The event streams have a
description of their own, an AsyncAPI 3.0 document at `GET /api/asyncapi`, with a YAML twin at
`/api/asyncapi.yaml`.

## Authentication

Every endpoint on this page is authenticated by whichever provider [`auth.mode`][auth.mode] selects. A client that has no
browser session can instead present an access token this deployment issued, as `Authorization: Bearer`—see
[API access tokens][api-tokens]. A token carries its user's identity, so [grants][authorization] and the `Allow`
header treat it exactly as they treat that person's session.

A token minted for `/mcp` is refused here, and one minted for the API is refused there. The 401 names the metadata
document for the resource you reached, so follow `resource_metadata` rather than assuming a location.

## Cross-origin writes

A browser attaches your session, certificate, or proxy-supplied identity to every request, whichever page started
it. So Cetacean refuses a write that a browser marks as coming from another site, with `403` and code
[`CSR001`](api/errors#CSR001). Anything other than `GET`, `HEAD`, and `OPTIONS` counts as a write.

Two kinds of origin pass: the one at [`server.public_url`][server.public_url], and each one listed in
[`server.cors.origins`][server.cors.origins]. A wildcard there lets browsers read from any origin but can't grant
writes, so name every origin that should be allowed to make them.

Requests that carry no browser headers, such as `curl` or a script, aren't affected. Neither are `/mcp` and the
OAuth `token`, `revoke`, and `register` endpoints, which authenticate from the request itself.

## Host names

A page can point its own name at Cetacean's address and then read the API as if it came from the same origin. To
stop that, a request whose `Host` isn't one of the [names Cetacean answers for][server.public_url] is refused with
`421` and code [`CSR002`](api/errors#CSR002).

## Content negotiation

Every resource URL serves JSON, HTML (the embedded [dashboard][dashboard]), SSE, or a feed format depending on what
the client asks for. There's no `/api/v1/` prefix; versioning lives in the media type.

### Resolution order

1. File extension appended to the path, which wins over everything else
2. `Accept` header, parsed per RFC 9110 with `q` values, parameters, and wildcards. A type is weighed by the most
   specific range matching the media type the endpoint sends, parameters included: `application/json;q=0, */*`
   refuses JSON, and `application/json;profile="x";q=0` doesn't. Naming an alias from the following table stands in
   for naming the type sent, unless that type is named too. An endpoint answers with the best of the types it serves.
   A wildcard never opens an event stream—ask for `text/event-stream` by name
3. `application/json` when the client sends `*/*` or no `Accept` header

A name can end in what looks like an extension. When a volume, config, secret, network, service, node, stack, or
plugin you may read is named exactly that, `/volumes/backup.json` is volume `backup.json`, negotiated by `Accept`. A
write to a resource never reads a suffix off its name: `DELETE /volumes/backup.json` removes `backup.json` or
answers `404`.

### Supported types

| `Accept` value | Extension | Result |
|---|---|---|
| `application/json` | `.json` | JSON |
| `application/vnd.cetacean.v1+json` | | JSON, versioned alias of `application/json` |
| `application/ld+json` | | JSON—every JSON response is a JSON-LD document |
| `text/html`, `application/xhtml+xml` | `.html` | The dashboard |
| `text/event-stream` | | SSE, on endpoints that support it |
| `application/atom+xml` | `.atom` | Atom feed |
| `application/feed+json` | `.feed` | JSON Feed 1.1 |
| `text/csv` | `.csv` | [CSV](#spreadsheets), on list endpoints |
| `application/vnd.jgf+json` | `.jgf` | JSON Graph Format, `/topology` only |
| `application/graphml+xml` | `.graphml` | GraphML, `/topology` only |
| `text/vnd.graphviz` | `.dot` | Graphviz DOT, `/topology` only |
| `application/yaml` | `.yaml`, `.yml` | [Compose file](#compose-export) on a stack or a service; the API descriptions |
| `application/spdx+json` | `.spdx` | An image's SPDX SBOM as stored, on [`sbom`](#image-attestations) only |
| `text/plain` | `.txt` | An image's Dockerfile, on [`dockerfile`](#image-attestations) only |

All negotiated responses include `Vary: Accept`. Requesting a type an endpoint can't produce returns
`406 Not Acceptable` with code [`API003`](api/errors#API003); asking for SSE on an endpoint without a stream
returns `406` with [`API001`](api/errors#API001). A graph format asked of a resource endpoint is refused the same
way—the table says which endpoints serve one.

The two API descriptions, `/api` and `/api/asyncapi`, also answer to the media types their specifications register,
such as `application/vnd.oai.openapi+json`, and serve YAML for `.yaml` or `application/yaml`.

The refusal is each endpoint's own. A document with a single representation—the
[OpenSearch description](#browser-search), the [API catalog](#api-catalog), the JSON-LD context—answers a client
asking for exactly the type it serves, whether or not that type appears in the preceding table.

```http tab
GET /services HTTP/1.1
Accept: application/json
```

```bash tab
curl -H "Accept: application/json" http://localhost:9000/services
```

### Compression

JSON, Atom, JSON Feed and CSV responses, and the `/topology` graph formats, are compressed when the request offers a
coding Cetacean serves and the body exceeds 1 KiB. Two codings are served, `zstd` and `gzip`, negotiated from
`Accept-Encoding` per [RFC 9110 §12.5.3](https://www.rfc-editor.org/rfc/rfc9110#section-12.5.3)—`q` values and
`*` included, and `zstd` preferred at equal weight. Smaller bodies are sent uncompressed regardless; there's
nothing to gain and a frame header to pay for.

`Vary: Accept-Encoding` is sent on every one of these responses whether or not anything was compressed, since what
they return does depend on the header. `Content-Encoding` appears only when a coding was actually applied.

A compressed representation carries its coding on the `ETag`, as a suffix inside the quotes:

```bash
curl -sD- -H 'Accept-Encoding: zstd' -o/dev/null http://localhost:9000/services/web
# < Content-Encoding: zstd
# < Vary: Accept-Encoding
# < ETag: "a1b2c3d4e5f60718-zstd"
```

The suffix distinguishes the two representations for caches, which is what
[RFC 9110 §8.8.3](https://www.rfc-editor.org/rfc/rfc9110#section-8.8.3) requires. It **doesn't** stop the validator
being used as [`If-Match`](#preconditions) on a write: the hash is always taken over the uncompressed body, so a
validator obtained under compression and one obtained without it agree, and either is accepted verbatim.

`/search` and its feeds are never compressed. The response repeats the query you sent back to you alongside content
filtered by your grants, and compressed length would leak whether a guessed query matched something you can see.

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

| Parameter | Description                              |
|-----------|------------------------------------------|
| `before`  | Return entries older than this cursor ID |
| `limit`   | Entries per page (default 50, max 200)   |

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

### Feed identity

An Atom feed identifies itself with a [tag URI](https://www.rfc-editor.org/rfc/rfc4151) naming the same host its own
links carry: [`server.public_url`][server.public_url] when set, otherwise the host the request arrived on. A tag URI is
meant to be permanent, so set `server.public_url` behind a reverse proxy—derived from the request, a feed's identity
changes with the hostname a reader happened to reach the server by.

### Feed autodiscovery

JSON responses on feed-capable endpoints carry a `Link` header with `rel="alternate"` for each feed type. The dashboard
injects an Atom `<link rel="alternate">` into the HTML `<head>` on resource, history, search, and recommendations
pages, so feed readers can find the feed from the page.

## Spreadsheets

List endpoints also serve [RFC 4180](https://www.rfc-editor.org/rfc/rfc4180) CSV, for the one consumer the other
formats don't serve: a person pasting the cluster into a spreadsheet.

```bash
curl -o services.csv http://localhost:9000/services.csv
curl -H 'Accept: text/csv' 'http://localhost:9000/tasks?filter=state == "failed"'
```

Responses carry `Content-Type: text/csv; charset=utf-8; header=present` and an
[RFC 6266](https://www.rfc-editor.org/rfc/rfc6266) `Content-Disposition`, so a browser opening `/services.csv` saves
`services-2026-09-11.csv` rather than rendering it. A task list hanging off a parent names it—`/nodes/{id}/tasks.csv`
saves `tasks-worker-1-2026-09-11.csv`.

A cell that starts with `=`, `+`, `-`, `@`, a tab or a carriage return is prefixed with `'`, so a spreadsheet shows a
label someone set to a formula instead of running it. A number such as `-1` is left as it is.

### Supported endpoints

- `/nodes`, `/services`, `/tasks`, `/stacks`, `/configs`, `/secrets`, `/networks`, `/volumes`
- `/nodes/{id}/tasks`, `/services/{id}/tasks`
- `/history`, `/recommendations`
- `/services/{id}/sbom`, `/tasks/{id}/sbom`, with the columns `name`, `version`, `ecosystem`, `license`, `purl`,
  `found_in`

Detail endpoints serve no CSV: one resource isn't a table. `/search` serves none either—its results are of mixed
type, and one header row can't describe them.

### Columns

A resource list renders the compact row the dashboard's own tables and the MCP `find` tool render: the name, the
state, and the one secondary fact that identifies the type—the image for a service, the role for a node, the node
for a task, the driver for a network or volume—plus replica counts where a replica count means something. The full
Docker object is what the JSON representation is for.

| Endpoint | Columns |
|---|---|
| `/services` | `name`, `stack`, `state`, `image`, `desired`, `running`, `id` |
| `/nodes` | `name`, `state`, `role`, `id` |
| `/tasks` | `name`, `state`, `node`, `id` |
| `/stacks` | `name`, `services`, `id` |
| `/configs`, `/secrets` | `name`, `stack`, `id` |
| `/networks` | `name`, `stack`, `driver`, `used`, `usable`, `id` (the addresses, empty without a figure) |
| `/volumes` | `name`, `stack`, `driver`, `id` |
| `/history` | `timestamp`, `type`, `action`, `name`, `id`, `summary`, `actor`, `provider`, `client`, `transport` |
| `/recommendations` | `severity`, `category`, `scope`, `target`, `resource`, `message`, `current`, `configured`, `suggested` |

### Pagination differs

`search`, `filter`, `sort` and [authorization][authorization] filtering apply exactly as they do to the JSON. Paging
does not: a CSV request that names no page renders **every** row, where the JSON would return the first 50. An export
truncated at a default carries nothing inside the file to say it was truncated, and the `Link` header that says so for
JSON isn't something a downloaded file keeps.

Naming a page still works, and then means what it means everywhere else:

```bash
curl 'http://localhost:9000/tasks.csv?limit=100&offset=200'
```

A [`Range` header](#range-header-pagination) isn't honored for CSV: that exchange answers `206` with a
`Content-Range`, and a download is always a plain `200`. Ask for a page with `limit` and `offset` instead.

## Compose export

A stack or a single service also serves a Compose file, for the consumer no other format serves: someone who wants
the running state as something they can read, keep in version control, or redeploy.

```bash
curl -o myapp.yaml http://localhost:9000/stacks/myapp.yaml
curl -H 'Accept: application/yaml' http://localhost:9000/services/r8m2x5nk3p7q
```

The promise is narrow and the file says so in a header comment: it redeploys to **the same running state on the same
cluster**. It isn't the file that originally created the stack, and it isn't portable on its own.

### What's external, and why

Secrets are never exported—that rule holds here as everywhere else. Configs aren't exported either, even though
their content is available: inlining it through Compose's `content:` field would have a redeploy create a new config
rather than reuse the one the running service is already mounting, which would break the preceding promise. Both are
referenced as `external: true`, so another cluster needs them created first.

Networks and volumes split. One carrying the stack's `com.docker.stack.namespace` label is the stack's own, and is
declared with its driver and options. Everything else—a shared `monitoring` overlay the services merely attach to—is
`external: true` under its full name. Getting this backwards is the single most likely way to produce a file that
reads correctly and fails to deploy, in both directions.

Names are shortened by the stack's own prefix, because `docker stack deploy` adds it back: the service Swarm calls
`myapp_api` is `api` in the file. A name another resource already spells in full is left long rather than collapsed
onto it—an owned `myapp_data` and an adopted `data` stay two volumes. A single-service export shortens nothing and
declares everything external, because a service creates none of what it references.

Anything the projection couldn't carry—a custom seccomp profile, a mount option Compose has no field for—is
listed in the header comment rather than dropped silently.

## Image attestations

When BuildKit pushes an image, it can attach provenance and an SBOM to it. Minimal provenance is on by default, and
the Docker Official Images carry both. Three endpoints read them from the image's registry, anonymously, when asked,
and cache the answers by digest. Nothing is verified: what they return is what the image says about itself.

| Endpoint | Returns |
|---|---|
| `/services/{id}/provenance`, `/tasks/{id}/provenance` | Per platform: the source repository and commit, the builder, build times, the inputs, the GitHub Actions job, and whether an SBOM and a Dockerfile exist. JSON only. |
| `/services/{id}/sbom`, `/tasks/{id}/sbom` | The packages of one platform's SPDX SBOM, as JSON or CSV, or the SPDX document itself |
| `/services/{id}/dockerfile`, `/tasks/{id}/dockerfile` | The Dockerfile that `mode=max` provenance embeds, with the layers each instruction produced, or the Dockerfile alone |

A service's endpoints describe the image its spec names; a task's describe the image it runs, which differs from the
service's mid-update. A task only offers the platforms its node can run.

The `provenance` endpoint always answers `200` for a resource the caller can read, with a `status`:

| `status` | Meaning |
|---|---|
| `available` | The `platforms` array is set |
| `none` | The image carries no attestations |
| `unpinned` | The image reference has no digest to look them up by |
| `private` | The registry refused anonymous access |
| `missing` | The registry no longer has the digest |
| `unavailable` | The registry couldn't be reached; `retryAfter` says when to ask again |
| `disabled` | `server.image_attestations` is off |

On an `available` image, a platform whose provenance couldn't be read carries `provenanceStatus`, with the same meaning
as `status`; the other platforms are unaffected.

`sbom` and `dockerfile` take a `platform` parameter, such as `linux/arm64`, needed when there is more than one
platform to choose from. Their errors are [`ATT001`](api/errors#ATT001) to [`ATT008`](api/errors#ATT008).

## Pagination

List endpoints page by query parameter or by HTTP `Range` header.

### Query parameters

| Parameter | Type   | Default | Description                                                   |
|-----------|--------|---------|---------------------------------------------------------------|
| `limit`   | int    | 50      | Items per page, capped at 200                                 |
| `offset`  | int    | 0       | Starting position                                             |
| `sort`    | string | none    | Sort field, varies by resource                                |
| `dir`     | string | `asc`   | Sort direction: `asc` or `desc`                               |
| `search`  | string | none    | Case-insensitive substring match on name                      |
| `filter`  | string | none    | [expr-lang](https://expr-lang.org/) expression, max 512 chars |

A `limit` or `offset` that isn't a whole number in range, a `dir` other than `asc` or `desc`, or a `sort` the
list doesn't offer is refused with `400` ([`API016`](api/errors#API016)). The same goes for a malformed
`before` or `limit` on `/history`, the feeds and `/search`, and a `since` or `until` on `/history` that isn't an
RFC 3339 timestamp.

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

List endpoints accept `Range: items=0-24` and answer `206 Partial Content` with `Content-Range: items 0-24/142`.
Every list response sets `Accept-Ranges: items`. An offset past the end returns `416` with `Content-Range: items
*/142`. Query parameters take precedence when both are present, and a malformed `Range` or one naming several
ranges is ignored. `Range: items 0-24`, without the `=`, is still accepted but deprecated.

Every page of a list carries the collection's `ETag`, as does the full answer. With `If-Range` naming it, any
page is served as long as the list is unchanged; otherwise you get the first page with a `200`.

### History pagination

`/history` pages by cursor rather than offset, because new entries arrive at the front while you read. A full page
carries a `Link` with `rel="next"`, whose `before` names the oldest entry on it. `search` matches names across the
whole log, not only the page you have, and stays on the `next` link along with `type` and `resourceId`.

```http tab
GET /history?search=web&limit=50 HTTP/1.1

GET /history?search=web&before=<entry-id>&limit=50 HTTP/1.1
```

```bash tab
curl "http://localhost:9000/history?search=web&limit=50"
curl "http://localhost:9000/history?search=web&before=<entry-id>&limit=50"
```

### History by time

`since` and `until` narrow `/history` to a window, as RFC 3339 timestamps: `since` is exclusive and `until`
inclusive, the same boundaries as the MCP tool `get_events`. Either can be left out. They combine with the other
filters in JSON and CSV, apply to the Atom and JSON Feed representations too, which read no other filter, and stay
on the `next` link, so paging never leaves the window. Encode the `+` of a UTC offset as `%2B`; a bare `+` in a query string reads as a space.

```http tab
GET /history?since=2026-09-05T14:00:00Z&until=2026-09-05T14:30:00Z HTTP/1.1
```

```bash tab
curl "http://localhost:9000/history?since=2026-09-05T14:00:00Z&until=2026-09-05T14:30:00Z"
```

### History as Grafana annotations

A Grafana JSON data source, such as the Infinity plugin, can draw the history as annotations on any dashboard.
Point it at `/history.json` with an [API access token](authentication#api-access-tokens) as its bearer token, and
pass the dashboard's range as `since=${__from:date:iso}&until=${__to:date:iso}&limit=200`. A page holds at most 200
entries, so a data source that doesn't follow the `next` link shows only the newest 200 of a busier range. Take the
rows from `items`, the time from `timestamp`, and the text from `name` and `action`. Grafana sees only the entries
the token's identity may read.

### Who made a change

An entry for a change made through Cetacean carries an `actor`: the identity that made it and how it arrived. An
entry with no `actor` is a change made somewhere else, such as with the Docker CLI.

```json
"actor": {
  "subject": "3f9c2a71-5d0e-4b8a-9c1f-7e2d6a4b8c90",
  "name": "Alice",
  "provider": "oauth",
  "client": "claude-desktop",
  "transport": "mcp"
}
```

`client` is the OAuth client ID when the call carried a token Cetacean issued. `transport` is `mcp` for the MCP
server. On the web API it's `dashboard` for a request the browser marks `Sec-Fetch-Site: same-origin` and that
carries no bearer token, and `rest` for anything else. The transport is a label for reading, not something a grant
checks. CSV exports carry the same fields as columns, and Atom and JSON Feed entries name the actor as their
author.

Swarm settings and plugin changes have no history entries, so they carry no actor either.

### Sort fields by resource

| Resource | Sortable fields                              |
|----------|----------------------------------------------|
| Nodes    | `hostname`, `role`, `status`, `availability` |
| Services | `name`, `mode`                               |
| Tasks    | `state`, `service`, `node`                   |
| Stacks   | `name`                                       |
| Configs  | `name`, `created`, `updated`                 |
| Secrets  | `name`, `created`, `updated`                 |
| Networks | `name`, `driver`, `scope`, `addresses`       |
| Volumes  | `name`, `driver`, `scope`                    |

### Filter fields by resource

Filter expressions use [expr-lang](https://expr-lang.org/) syntax and must evaluate to a boolean. Operators: `==`,
`!=`, `<`, `>`, `<=`, `>=`, `contains`, `startsWith`, `endsWith`, `in`, `not in`, `&&`, `||`, `!`.

| Resource | Fields                                                                                                                              |
|----------|-------------------------------------------------------------------------------------------------------------------------------------|
| Nodes    | `id`, `name` (hostname), `state` (`ready`/`down`/`unknown`), `role` (`manager`/`worker`), `availability` (`active`/`pause`/`drain`) |
| Services | `id`, `name`, `image`, `mode` (`replicated`/`global`/`replicated-job`/`global-job`), `stack`                                        |
| Tasks    | `id`, `state`, `desired_state`, `image`, `exit_code` (int), `error`, `service` (ID), `node` (ID), `slot` (int)                      |
| Stacks   | `name`, `services`, `configs`, `secrets`, `networks`, `volumes` (all counts)                                                        |
| Configs  | `id`, `name`                                                                                                                        |
| Secrets  | `id`, `name`                                                                                                                        |
| Networks | `id`, `name`, `driver`, `scope` (`swarm`/`local`)                                                                                   |
| Volumes  | `name`, `driver`, `scope`                                                                                                           |
| Ports    | `published` (int), `target` (int), `protocol`, `mode` (`ingress`/`host`), `service` (name), `stack`, `nodes` (count, 0 for ingress) |

Task `state` takes the Docker task states: `new`, `allocated`, `pending`, `assigned`, `accepted`, `preparing`,
`ready`, `starting`, `running`, `complete`, `shutdown`, `failed`, `rejected`, `remove`, `orphaned`. `exit_code` is
a number, `0` until the task reaches a terminal state, so `exit_code > 0` selects the tasks that exited with an error.
A task that exited cleanly needs its state as well: `state == "complete" && exit_code == 0`.

An expression that names a field the resource doesn't have, such as the typo `staet == "failed"`, or compares
values of different types, such as `exit_code != "0"`, is refused with `400 FLT002`. An expression that takes too
long to evaluate over the whole list, builds a string longer than 1 MiB, or runs a costly pattern over a long string
is refused with `400 FLT004`.

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

## Resource identifiers

Detail paths accept a resource's ID or its name. A name is answered with `307 Temporary Redirect` to the
canonical, ID-addressed URL:

```bash
curl -i -H 'Accept: application/json' http://localhost:9000/services/shop_web
# < HTTP/1.1 307 Temporary Redirect
# < Location: /services/x3k9m2p8q1w7
```

`307` preserves the method and the body, so writes may be addressed by name too: `PUT /services/shop_web/scale`
reaches the scale endpoint with its payload intact. Most clients follow the redirect on request (`curl -L`).

Volumes and stacks are keyed by name already, so their paths never redirect. A task's name is its service's name and
slot, `shop_web.1`, or node ID for a global service; it names the live task in that slot, and resolves only for a
caller who may read the service.

A name matching more than one resource is answered `409 Conflict` with [`API015`](api/errors#API015), whose
detail lists every ID the request could have meant.

## Errors

Errors follow [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details, with Content-Type
`application/problem+json` and `Cache-Control: no-store`. A problem's `@context` adds the RFC 9457 members to the
shared context, which leaves `type`, `status` and `detail` to the resources that use those names for their own values.

```json
{
  "@context": [
    "/api/context.jsonld",
    {
      "type": { "@id": "urn:ietf:rfc:9457#type", "@type": "@id" },
      "title": "urn:ietf:rfc:9457#title",
      "status": "urn:ietf:rfc:9457#status",
      "detail": "urn:ietf:rfc:9457#detail",
      "instance": { "@id": "urn:ietf:rfc:9457#instance", "@type": "@id" }
    }
  ],
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
  "@context": ["/api/context.jsonld", { "...": "as above" }],
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
| Resource changed between your read and your write | 409 | [`SVC001`](api/errors#SVC001), [`NOD002`](api/errors#NOD002), [`CFG005`](api/errors#CFG005), [`SEC005`](api/errors#SEC005), [`SWM012`](api/errors#SWM012) | Re-read the resource and retry |
| Endpoint above the configured [operations level][operations-level] | 403 | [`OPS001`](api/errors#OPS001) | Raise the operations level |
| [Grants][authorization] deny a read, or a write to a resource you can read | 403 | [`ACL001`](api/errors#ACL001), [`ACL002`](api/errors#ACL002) | The response names the resource and permission checked |
| [Grants][authorization] deny a write to a resource they don't let you read either | 404 | The resource's not-found code, such as [`SVC003`](api/errors#SVC003) | Ask for a grant on the resource |
| Cross-origin write from an origin that isn't allowed | 403 | [`CSR001`](api/errors#CSR001) | Add the origin to [`server.cors.origins`][server.cors.origins] |
| `Host` names none of Cetacean's names | 421 | [`CSR002`](api/errors#CSR002) | Add the name to [`server.allowed_hosts`][server.allowed_hosts] |
| `PATCH` sent with the wrong `Content-Type` | 415 | [`API004`](api/errors#API004) | Use `application/json-patch+json` or `application/merge-patch+json` |
| Docker daemon unreachable | 503 | [`ENG001`](api/errors#ENG001) | Check the socket and the daemon |
| Name in the path identifies more than one resource | 409 | [`API015`](api/errors#API015) | Address the resource by ID; the detail lists the candidates |

## Caching

JSON responses carry an `ETag` and `Cache-Control: no-cache`; `GET /swarm/unlock-key` sends `no-store` instead, so no cache keeps the key. `GET /auth/whoami` sends `no-store` and no `ETag`: it describes the caller, and a validator on it would invite the sharing `no-store` forbids. Use `If-None-Match` for conditional requests:

```bash
curl -v http://localhost:9000/services
# < ETag: "3a7f..."

curl -H 'If-None-Match: "3a7f..."' http://localhost:9000/services
# < HTTP/1.1 304 Not Modified
```

A service's and a node's detail also set `Last-Modified` from the resource's update timestamp, or for a service from
when a newer image was found if that is later, and honor `If-Modified-Since`. When both conditional headers are present, `If-None-Match` wins. A config, secret, network,
volume, or task lists what refers to it, which changes without its timestamp moving, so those carry only the `ETag`.

A read honors `If-Match`, and `If-Unmodified-Since` where it sends `Last-Modified`, as well: when one doesn't hold, the
answer is `412` ([`API013`](api/errors#API013)). They are evaluated in the order
[RFC 9110 §13.2.2](https://www.rfc-editor.org/rfc/rfc9110#section-13.2.2) gives: `If-Match`, or `If-Unmodified-Since`
without it, then `If-None-Match`, or `If-Modified-Since` without it, and `If-Range` last.

`/api` and `/api/context.jsonld` return `Cache-Control: public, max-age=3600`; `/api/scalar.js` returns `max-age=86400`.
SSE and streaming endpoints set no caching headers.

## Response headers

`Allow` on `GET` and `HEAD` responses lists the methods available for that resource under the current
[operations level][operations-level] and your [grants][authorization]. Inspect it before attempting a
write. It names the resource's own methods only: `DELETE` on `/services/{id}` removes the service.

A resource's sub-resources are linked instead. A detail response carries a `Link` with `rel="edit"` for each
sub-resource you may write, so `</services/{id}/scale>; rel="edit"` means `PUT /services/{id}/scale` is open to
you. A sub-resource the operations level or your grants close isn't linked.

```http
Allow: GET, HEAD, DELETE
Link: </services/abc/scale>; rel="edit", </services/abc/labels>; rel="edit"
```

`Accept-Patch` lists the patch formats a resource accepts, and appears where its own `Allow` carries `PATCH`. Labels and
service `env` take `application/json-patch+json, application/merge-patch+json`; every other patchable sub-resource takes
`application/merge-patch+json` alone.

A JSON Patch `path` is a [JSON Pointer](https://www.rfc-editor.org/rfc/rfc6901) to one key: write `/` in a key as
`~1` and `~` as `~0`, so label `app.kubernetes.io/name` is `/app.kubernetes.io~1name`.

A service's `ports`, `configs`, `secrets`, `networks` and `mounts` each take a merge patch naming that one list,
spelled exactly so; any other member answers `400`. A patch without the member changes nothing; `null` or `[]` empties
the list.

A method a path doesn't serve answers `405 Method Not Allowed`, with an `Allow` naming the methods it does. That
includes a `GET` on a path only writes are routed at, such as `/services/{id}/scale`, from a client whose `Accept`
rules out HTML; a browser there gets the dashboard. `OPTIONS` on any routed path answers `204` with that `Allow`, and
with `Accept-Patch` where it offers a `PATCH`. Both list only the writes your operations level and grants open, as
a `GET`'s `Allow` does. A `415` for a patch format the path doesn't take carries
`Accept-Patch` too.

Write endpoints honor [RFC 7240](https://www.rfc-editor.org/rfc/rfc7240) `Prefer: return=minimal`, answering
`204 No Content` (or `201 Created` for a create) with `Preference-Applied: return=minimal` instead of the updated
resource.

A write answering with a body checks `Accept` once you're authorized and before it changes anything: an `Accept`
that rules out both JSON and `application/problem+json` answers `406` ([`API003`](api/errors#API003)) and the write
doesn't happen. The writes that always answer `204` skip the check, as does one sent with `Prefer: return=minimal`
and no `wait` or `respond-async`, except `POST /plugins` and `POST /plugins/privileges`, which always send a body.

Every response sets `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, and `Content-Security-Policy`. HSTS
is added when TLS is enabled.

## Waiting for a change to take effect

Docker accepts a service change the moment you ask for it, which says nothing about whether it worked—the image may
still be pulling, or a placement constraint may be unsatisfiable. Send `Prefer: wait=<seconds>` to hold the response
until the cluster has actually settled on the change:

```bash
curl -X PUT http://localhost:9000/services/abc123/scale \
  -H 'Content-Type: application/json' \
  -H 'Prefer: wait=30' \
  -d '{"replicas":5}'
```

| Request | Response |
|---|---|
| no `Prefer` | unchanged |
| `wait=30`, service settles in time | `200` with the settled service, plus `Preference-Applied: wait=30` |
| `wait=30`, service doesn't settle | `202` with the last progress line, and no `Preference-Applied` |
| `wait=600` | clamped to the 300 second ceiling; `Preference-Applied: wait=300` reports the wait applied |
| `respond-async` | `202` straight away, plus `Location` and `Preference-Applied: respond-async` |
| `respond-async, wait=10` | waits up to 10 seconds, then `202` with `Location` |
| `return=minimal, wait=30` | waits, then `204`; `Preference-Applied` names both preferences |

At most 32 writes wait at once. Past that, a `wait` is answered as `respond-async` would be, with a `Retry-After`.

A `200` from an honored wait describes the service as it settled, read back after convergence—not the snapshot Docker
returned when it accepted the write, whose `UpdateStatus` is still mid-rollout and whose `Version` a follow-up write
would collide on.

A `202` carries `Location` pointing at the service itself, which is where the rollout can be followed: its
`UpdateStatus` reports convergence, and the same URL opens a [live stream](#real-time-events) with
`Accept: text/event-stream`. The body is the service as Docker returned it, plus a `progress` field holding the last
convergence line observed—`waiting: 2/5 replicas running`. `return=minimal` doesn't apply on this path: there's a
progress line to deliver, so the `202` carries a body and doesn't name `return=minimal` in `Preference-Applied`.
Whether that preference is honored therefore depends on how quickly the cluster settles. A client that hangs up
cancels its own wait.

The preferences apply to the five service endpoints that change what the cluster has to schedule: `scale`, `image`,
`endpoint-mode`, `rollback` and `restart`. Node availability takes no `wait`—draining is a different rule,
with a different notion of what "settled" means.

## Real-time events

Send `Accept: text/event-stream` to any resource list or detail URL to open a stream scoped to that path.

### Per-resource streams

A list stream carries events for one resource type; the service list stream adds task and node events, which move
the counts its items carry. A detail stream carries events for one resource plus the resources
that belong to it: a node stream includes that node's tasks, a service stream includes that service's tasks, a network
stream includes the tasks attached to it and every service event, plus node events on `ingress`, and a stack stream
includes its member services, tasks, configs, secrets, networks, and volumes.

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
| `full_sync` | Sent as a `sync` event when the stream couldn't be replayed from the client's cursor, or the client fell behind and missed events. Refetch. |

Events arriving within the batch interval ([`server.sse.batch_interval`][server.sse.batch_interval], default 100 ms)
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

Each event carries an `id:`. Treat it as opaque: it names a position in the change history of one Cetacean process.
EventSource clients send `Last-Event-ID` on reconnect and the server replays what they missed from the change history.
Detail and stack streams can't be replayed, and neither can a cursor older than the history buffer, one written
before Cetacean restarted, or one from before Cetacean last re-read the whole cluster; all four send a `sync` event
telling the client to refetch.
A client that reads too slowly to take every event gets a `sync` event in place of the ones it missed. One that stops
reading altogether is disconnected once a write to it has been blocked for 30 seconds, and reconnects from its last
cursor. Network buffers absorb writes first, so that can be well after the client stopped reading.

### Metrics streams

`GET /metrics` with `Accept: text/event-stream` pushes periodic updates instead of proxying one query to Prometheus.

```http tab
GET /metrics?query=up&step=15&range=3600 HTTP/1.1
Accept: text/event-stream
```

```bash tab
curl -H "Accept: text/event-stream" "http://localhost:9000/metrics?query=up&step=15&range=3600"
```

| Event     | Description                                                                 |
|-----------|-----------------------------------------------------------------------------|
| `initial` | Full range query result on connect, same shape as Prometheus `query_range`. |
| `point`   | Single instant query result appended at each tick.                          |

Append `point` events to the data you already hold to build a rolling window.

Under an [ACL policy][authorization], the stream and the Prometheus proxy (`GET /metrics`, `GET /metrics/labels`,
`GET /metrics/labels/{name}`) answer `403 ACL001` unless the caller holds `read` on the bare resource `*`.

The proxy forwards a Prometheus answer of up to 10 MiB. A longer one is refused whole with `502`
([`MTR008`](api/errors#MTR008)) rather than cut short; on a stream, the same answer is a `query_error` event. A query
Prometheus refuses, because it doesn't parse or asks for too many points, is `400`
([`MTR009`](api/errors#MTR009)) with Prometheus' reason, not a `502`.

### Stream contract

`GET /api/asyncapi` describes all twenty streams as an [AsyncAPI 3.0](https://www.asyncapi.com/) document: their
channels, messages, and cursor semantics. Add `.json` or `.yaml`, or negotiate on `Accept`. Browse it in
[AsyncAPI Studio](https://studio.asyncapi.com/).

Two details the preceding examples don't show: a `batch` payload is an **array** of envelopes, and a **replayed** event
carries no `resource`.

## Connection limits

There's no general rate limiting. Concurrent streams and log reads are capped, and a request over the cap returns
`429 Too Many Requests` with a `Retry-After` of 5 to 14 seconds. The value steps between refusals, so clients
turned away together aren't told to come back together; none is told less than 5.

| Stream                                                  | Limit | Code     |
|---------------------------------------------------------|-------|----------|
| SSE event clients (`/events` and per-resource streams)  | 256   | `SSE001` |
| SSE event clients from one caller                       | 64    | `SSE001` |
| Log streams (`/services/{id}/logs`, `/tasks/{id}/logs`) | 128   | `LOG001` |
| Log streams from one caller                             | 32    | `LOG001` |
| Log reads (the same paths as JSON, and MCP `get_logs`)  | 32    | `LOG009` |
| Log reads from one caller                               | 8     | `LOG009` |
| Metrics streams (`/metrics` as SSE)                     | 64    | `MTR005` |

A caller is the signed-in identity, or the client address under the `none` auth mode. Behind a reverse proxy, set
[`server.trusted_proxies`][server.trusted_proxies] so that address is the client's rather than the proxy's.

## Endpoints

`GET /api` serves the full OpenAPI spec with request and response schemas, and the
[API explorer][api-explorer] renders it interactively. The following tables are the shape of the surface.

### Reads

| Area | Endpoints |
|---|---|
| Entry point | `/`, `/index` |
| Nodes | `/nodes`, `/nodes/{id}`, `/nodes/{id}/tasks`, `/nodes/{id}/labels`, `/nodes/{id}/role`, `/nodes/{id}/drain-impact` |
| Services | `/services`, `/services/{id}`, `/services/{id}/tasks`, `/services/{id}/logs` |
| Service spec sections | `/services/{id}/` + `env`, `labels`, `resources`, `healthcheck`, `placement`, `ports`, `update-policy`, `rollback-policy`, `log-driver`, `configs`, `secrets`, `networks`, `mounts`, `container-config`, `mode`, `endpoint-mode` |
| Tasks | `/tasks`, `/tasks/{id}`, `/tasks/{id}/logs` |
| [Image attestations](#image-attestations) | `/services/{id}/` and `/tasks/{id}/` + `provenance`, `sbom`, `dockerfile` |
| Stacks | `/stacks`, `/stacks/summary`, `/stacks/{name}` |
| Configs and secrets | `/configs`, `/configs/{id}`, `/configs/{id}/labels`, `/secrets`, `/secrets/{id}`, `/secrets/{id}/labels`, `/secret-entries/{id}` |
| Networks and volumes | `/networks`, `/networks/{id}`, `/volumes`, `/volumes/{name}` |
| Ports | `/ports` |
| Plugins | `/plugins`, `/plugins/{name}`, `/swarm/plugins` |
| Cluster | `/cluster`, `/cluster/metrics`, `/cluster/capacity`, `/swarm`, `/disk-usage` |
| Cross-resource | `/search?q=`, `/history`, `/events`, `/recommendations`, `/topology`, `/profile` |
| Metrics | `/metrics`, `/metrics/status`, `/metrics/labels`, `/metrics/labels/{name}` |
| Documentation | `/api`, `/api/openapi.yaml`, `/api/asyncapi`, `/api/asyncapi.yaml`, `/api/context.jsonld`, `/api/errors`, `/api/errors/{code}` |
| Discovery | `/.well-known/api-catalog`, `/opensearch.xml` |
| Identity | `/auth/whoami`, plus `/auth/login` and `/auth/callback` in OIDC mode |
| Meta | `/-/health`, `/-/ready`, `/-/metrics`, `/-/licenses`, `/-/licenses/texts/{id}`, `/-/notices`, `/-/sbom.cdx`, `/-/docker-latest-version` |

`GET /search` takes `q` (required, max 200 characters) and `limit` (per type, default 3; `0` or a value above 1000
returns up to 1000). `POST /-/resync` forces a full refetch from the Docker socket; unlike the other
`/-/` endpoints it requires authentication and a grant, because each call sweeps the whole Docker API.
A call made while a re-sync is already running waits for that one and shares its result, and it isn't
gated on the operations level—it re-reads the cluster and never changes it. A failed re-sync answers with
problem details: `504` ([`ENG005`](api/errors#ENG005)) when Docker didn't answer in time, otherwise `502`
([`ENG006`](api/errors#ENG006)), naming what each list answered.

`GET /nodes/{id}/drain-impact` assesses a drain before you make it: each service with a task on the node is
`movable`, with an edge to every node that could take it, `stranded`, with what blocked it in `detail`, or `global`.
It's the same assessment as the MCP `get_topology` drain-impact view, and the dashboard shows it before a drain.

`GET /topology` as JSON Graph Format holds two graphs. `network` has a vertex per service and an edge between two
services sharing an overlay network. `placement` has a vertex per node and per service, and a placement entry per
service listing the nodes its tasks run on. A node vertex carries `address`, and a service vertex `networks`, the
overlays it's attached to as `id` and `name`, limited to the networks you may read.

Each `GET /services` item carries `RunningTasks` and `DesiredTasks`: the replicas, the ready, active nodes a global
service's constraints admit, or the completions a job's run needs. A job's item adds `CompletedTasks` and
`FailedTasks`, counting only its current run; a rejected task counts as failed.

`GET /services/{id}` carries `imageUpdate`, and each `GET /services` item `ImageUpdate`, when the service's image tag
names a newer image in its registry than the digest the service runs: `image`, `currentDigest`, `latestDigest`,
`checkedAt`, and `suggestedImage`, the tag pinned to the newer digest, ready for `PUT /services/{id}/image`. It's absent
whenever there is nothing to report, including when [`server.image_check`](configuration#server.image_check) is off.

`GET /configs` and `GET /secrets` list, by default, the newest version of each group of
[versions](dashboard#versions) and every older one a service's current spec still uses; `?versions=all` lists
every version, and `?versionsOf={id}` only those grouped with that ID, newest first unless `sort` orders them
otherwise. A `search` or `filter` matches every version, and a CSV export holds every version with `version of`,
`newest` and `used by` columns, both unless `?versions=newest` says otherwise; a config or secret with no other
version is its own newest. Any other `versions` value answers `400`
([`API016`](api/errors#API016)). The total, the pagination links and the ETag follow what is listed.

`GET /configs/{id}` and `GET /secrets/{id}` list in `services` every service whose spec or previous spec uses it,
marking with `rollback` one that only a rollback would need, and count them in `usedBy`; each list item counts them
in `UsedBy`. For one of several versions, the detail carries `versionOf`, naming the group, and
`versionInferred` when it was matched by name rather than declared with the `cetacean.version-of` or `com.matchory.deployment.name` label; the newest
carries `newestVersion` and `olderVersions`, the number of older ones no current spec uses, which a default
listing hides; an older one still in use carries `inUse`. A list item spells them `VersionOf`, `VersionInferred`,
`NewestVersion`, `OlderVersions` and `InUse`. All of them count only the configs, secrets, and services
the caller may read, so the newest version a caller can read is the newest it is shown.

`GET /networks/{id}` carries `addresses`, and each `GET /networks` item `Addresses`, for a swarm network on the
default IPAM driver with an IPv4 pool: `usable`, `used`, and what holds them—`vips`, `taskAddresses` and
`loadBalancers`. `excludesAttachedContainers` is true on an attachable network, whose standalone containers aren't
counted. Sorting by `addresses` orders by the share in use, with networks that have no figure last either way. The
counts cover every service and task on the network, including ones the caller can't read. [Network
addresses](dashboard#network-addresses) explains the figure.

`GET /services/{id}` carries `links` when the service declares [custom links](integrations#custom-links), and
`GET /stacks/{name}` the links of all its services, exact duplicates merged. Each has an `id`, a `title`, and either
an `href`, for an `http` or `https` URL, or a `text`, for any other value. A stack's links also name the `services`
declaring each one.

`GET /-/health` carries `status`, `version`, `commit`, `buildDate`, and the configured `operationsLevel`, and a
`watcher` object reporting whether Cetacean is still tracking the cluster: `connected` for the Docker event
stream, and `lastSyncAt` / `lastSyncAgeSeconds` for the last successful read. `/-/metrics` carries the same as
`cetacean_watcher_connected`, `cetacean_cache_last_sync_timestamp_seconds` and `cetacean_cache_sync_failures_total`.

### Writes

Each row gives the minimum [operations level][operations-level] the endpoint needs. Every write also
passes the per-resource [write grant][authorization] check.

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
| `PUT /services/{id}/endpoint-mode` | 3 |
| `DELETE /services/{id}` | 3 |
| `PATCH /nodes/{id}/labels` | 2 |
| `PUT /nodes/{id}/availability` | 3 |
| `PUT /nodes/{id}/role` | 3 |
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
| `DELETE /tokens/{subject}` | 3 |
| `POST /plugins/{name}/enable` | 2 |
| `POST /plugins/{name}/disable` | 2 |
| `PATCH /plugins/{name}/settings` | 2 |
| `POST /plugins` | 3 |
| `POST /plugins/privileges` | 3 |
| `POST /plugins/{name}/upgrade` | 3 |
| `DELETE /plugins/{name}` | 3 |

`POST /plugins` and `POST /plugins/{name}/upgrade` grant only the privileges their body names, in the `privileges`
array `POST /plugins/privileges` returned. A request without one is refused with `400` (`PLG005`), and one whose plugin
now asks for a different set—a tag republished after the review—with `409` (`PLG006`).

One read is held to a tier as well: `GET /swarm` includes the join tokens only at level 3, and only
to a caller with write on `swarm:cluster`. Below that, `JoinTokens` comes back empty.

Three level-2 endpoints refuse part of a request below level 3: `PATCH /services/{id}/mounts` a mount the service
doesn't already have that is a bind, a named pipe, a volume with driver options, or a named volume the manager doesn't
hold as a plain `local` volume; `PATCH /services/{id}/container-config` a capability added to `capabilityAdd`; and
`PATCH /plugins/{name}/settings` a setting that names one of the plugin's mounts or devices, since it sets a host path.

A service with host access—a bind, named-pipe, or cluster mount, a volume with driver options, or an added
capability—needs level 3 for any change to what runs with it: `PUT /services/{id}/image`, a
`PUT /services/{id}/placement` that changes it, a `PATCH /services/{id}/container-config` that changes `command`,
`args`, `dir`, `user`, or `groups`, a `PUT` or `PATCH /services/{id}/healthcheck` that changes `Test`, and a `PATCH` of
its `env`, `labels`, `ports`, `networks`, `secrets`, `configs`, or `mounts` that changes them; resending the same
values, in any order, isn't a change. Service labels count because a reverse proxy routes by them; the container labels
in `container-config` don't. `POST /services/{id}/rollback` needs it when either the current or the previous spec has
host access. Below that they answer `403` (`OPS001`). A named volume on a worker can't be inspected from the manager, so
host access that comes only through one isn't held to this rule.

### Preconditions

Every write endpoint whose exact path also serves a `GET` accepts an optional `If-Match` request
header ([RFC 9110 §13.1.1](https://www.rfc-editor.org/rfc/rfc9110#section-13.1.1)). Supply the
`ETag` a `GET` on that same path returned; if the resource has changed since, the write is
refused with `412 Precondition Failed` (error code `API013`) instead of being applied. The header
is always optional—omit it and the write proceeds exactly as it did before this existed.

The same endpoints evaluate `If-None-Match` and `If-Unmodified-Since`, in the RFC 9110 §13.2.2 order. `If-None-Match:
*` refuses the write while the resource exists, and a tag refuses it while that tag is current, both with `412`.
`If-Unmodified-Since` is compared with the `Last-Modified` a `GET` sends, so only `DELETE /services/{id}` and
`DELETE /nodes/{id}` have a date to compare it with; elsewhere it's ignored, as is a value that isn't a single valid
date, and it's ignored whenever `If-Match` is present.

A `412` always means the resource moved. A resource that is gone answers `404`, the same as it
would without the header—[RFC 9110 §13.2.1](https://www.rfc-editor.org/rfc/rfc9110#section-13.2.1)
keeps the precondition out of the way of an answer that was never going to be a success. Where the
current representation can't be read at all—`DELETE /plugins/{name}` inspects the daemon rather
than the cache—the write answers `503` (`ENG001`) or `500` (`ENG004`) instead, so an unreachable
daemon isn't reported as a stale `ETag`.

On an update to a service, node, config or secret, the check and the write are one step: the write
names the version the check validated, and Docker refuses it, again with `412`, if anything changed
in between. Docker's removals take no version, so a `DELETE` is checked just before it runs, and a
change in that instant goes unnoticed.

29 endpoints support it: `PATCH /services/{id}/env`, `PATCH /services/{id}/labels`,
`PATCH /services/{id}/resources`, `PUT`/`PATCH /services/{id}/healthcheck`,
`PUT /services/{id}/placement`, `PATCH /services/{id}/ports`,
`PATCH /services/{id}/update-policy`, `PATCH /services/{id}/rollback-policy`,
`PATCH /services/{id}/log-driver`, `PATCH /services/{id}/configs`,
`PATCH /services/{id}/secrets`, `PATCH /services/{id}/networks`,
`PATCH /services/{id}/mounts`, `PATCH /services/{id}/container-config`,
`PUT /services/{id}/endpoint-mode`, `DELETE /services/{id}`,
`PATCH /nodes/{id}/labels`, `PUT /nodes/{id}/role`, `DELETE /nodes/{id}`,
`PATCH /configs/{id}/labels`, `DELETE /configs/{id}`, `PATCH /secrets/{id}/labels`,
`DELETE /secrets/{id}`, `DELETE /networks/{id}`, `DELETE /volumes/{name}`,
`DELETE /tasks/{id}`, `DELETE /stacks/{name}`, `DELETE /plugins/{name}`.

19 refuse every `If-Match`, `*` included, with `412`: they are action-style endpoints with no `GET` at that exact
path, so there is no `ETag` the header could match, and `If-None-Match` finds none to refuse. They are `PUT /services/{id}/scale`,
`PUT /services/{id}/image`, `POST /services/{id}/restart`, `POST /services/{id}/rollback`,
`PUT /nodes/{id}/availability`, `POST /plugins/{name}/enable`, `POST /plugins/{name}/disable`,
`POST /plugins/{name}/upgrade`, `PATCH /plugins/{name}/settings`, `POST /plugins/privileges`, `PATCH /swarm/ca`,
`PATCH /swarm/dispatcher`, `PATCH /swarm/encryption`, `PATCH /swarm/orchestration`, `PATCH /swarm/raft`,
`POST /swarm/rotate-token`, `POST /swarm/rotate-unlock-key`, `POST /swarm/force-rotate-ca`, and `POST /swarm/unlock`.
A missing service, node or plugin still answers `404`. Send these without the header.

Four ignore it. `POST /auth/logout` ends a session rather than changing the cluster. `POST /configs`,
`POST /secrets` and `POST /plugins` are excluded deliberately: their nearest `GET` is the collection listing, and
its `ETag` turns over on any member change, which would make "create only if the collection is unchanged" a
precondition almost nothing could ever satisfy.

## MCP server

Cetacean can also serve its cluster view over the Model Context Protocol. Set [`mcp.enabled`][mcp.enabled] to
`true` and the server mounts at `/mcp`. See [MCP server][mcp] for transport, authorization, and prompts, and [MCP
tools and resources][mcp-tools] for the catalog.

## Self-discovery

Every response outside the `/-/` meta endpoints carries [RFC 8631](https://www.rfc-editor.org/rfc/rfc8631) `Link`
headers:

```http
Link: </api>; rel="service-desc"; type="application/json", </api/asyncapi>; rel="service-desc"; type="application/vnd.aai.asyncapi+json;version=3.0.0", </api/context.jsonld>; rel="describedby", </.well-known/api-catalog>; rel="api-catalog"
```

The two `service-desc` links are told apart by `type`: `/api` describes the request/response API, `/api/asyncapi`
the event streams. `describedby` points at the JSON-LD context document, and `api-catalog` at the
[API catalog](#api-catalog).

### Browser search

`GET /opensearch.xml` is an [OpenSearch 1.1](https://github.com/dewitt/opensearch/blob/master/opensearch-1-1-draft-6.md)
description document, advertised by the dashboard with a `<link rel="search">`. Browsers that support the convention
can be taught a keyword, so the address bar searches the cluster directly; Firefox still offers this, Chromium has
narrowed it. The document also carries Atom and JSON templates for the same search, and requires authentication like
any other endpoint.

## Request ID

Every response carries a `Request-Id` header. Send your own in the `Request-Id` request header (max 64 printable ASCII
characters) or the server generates one. The value appears in error responses as `requestId` and in the server logs.

## Entry point

`GET /` is the web API's entry point. A browser gets the [dashboard][dashboard]; a JSON client gets a JSON-LD
document naming every top-level collection, so knowing the origin is enough to find everything else:

```json
{
  "@context": "/api/context.jsonld",
  "@id": "/",
  "@type": "EntryPoint",
  "name": "Cetacean",
  "resources": {
    "nodes": { "@id": "/nodes" },
    "services": { "@id": "/services" }
  },
  "version": "0.14.0"
}
```

`/index` redirects here permanently, keeping any `.json` or `.html` suffix—`/index.json` lands on `/.json`, since
the suffix is the only thing naming the representation.

The [API catalog](#api-catalog) is unauthenticated and points here; this document is not. Discovery is public,
the API behind it isn't.

## API catalog

`GET /.well-known/api-catalog` lists the APIs this deployment publishes, as an
[RFC 9264](https://www.rfc-editor.org/rfc/rfc9264) linkset served as `application/linkset+json`
([RFC 9727](https://www.rfc-editor.org/rfc/rfc9727)). No authentication required.

Each `item` names an API; the contexts beside it carry that API's `service-desc`, `service-doc`, `describedby` and
`status` links. The MCP server appears only when [`mcp.enabled`][mcp.enabled] is set, and its authorization metadata
only when [`oauth.enabled`][oauth.enabled] is set. The web API's own authorization metadata needs
[`oauth.api_tokens`][oauth.api_tokens] as well.

URIs are absolute—set [`server.public_url`][server.public_url] behind a reverse proxy.

## Compatibility

Cetacean follows [semantic versioning](https://semver.org/) from 1.0 onward. A change is breaking when a client that
worked against an earlier 1.x release stops working, or quietly gets a different answer, without changing anything on
its side. Breaking changes wait for the next major release. Until 1.0, a minor release can still break things, and
the changelog marks each one **Breaking**.

### What a 1.x release keeps

- **Routes:** each path, its methods and status codes, the `Allow` header, `rel="edit"` targets, and the media
  types the route serves. The [API explorer][api-explorer] lists them.
- **Response bodies:** field names, types and meaning, the `@type` of each document, and every term
  `/api/context.jsonld` defines.
- **Errors:** every problem code and the status it answers with, as the [error reference](api/errors) lists them.
- **Pagination, filtering and sorting:** the query parameters, `Link` relations, `Range: items=` and
  `Content-Range`, the `?filter=` grammar and its fields, and the `?sort=` fields.
- **Feeds and spreadsheets:** which endpoints serve them, and CSV column names and order.
- **Event streams:** event names, payload shapes, `Last-Event-ID` replay and the `sync` and `full_sync` events, as
  `/api/asyncapi` describes them.
- **MCP:** tool, argument and prompt names, argument types, resource URIs, and widgets. See
  [MCP tools and resources][mcp-tools.compatibility] for what that includes.
- **Configuration:** each setting's TOML path, environment variable, flag and default, and the `_FILE` variants.
  The [configuration reference][configuration] is the list.
- **Authorization policies:** grant syntax, resource types, permissions and audience prefixes, as
  [Authorization][authorization] describes them, and the [operations level][operations-level] each write needs.
- **Self-metrics:** the `cetacean_*` metric and label names served at `/-/metrics`.
- **Stored state:** the snapshot, the OAuth token store and registered OAuth clients under
  [`storage.data_dir`][storage.data_dir] stay readable by every later 1.x release.
- **The `healthcheck` subcommand** and its exit codes.

### What it doesn't

- **Additions.** A new field, route, tool, argument, event, setting, error code or metric ships in a minor release.
  Clients must ignore what they don't recognize.
- **Wording.** Problem `title` and `detail`, tool descriptions, prompt text, log messages and the dashboard's text
  can change in any release.
- **The dashboard.** Its pages and any URL beyond the resource routes ship with the server they belong to.
- **Order.** List items without `?sort=`, members of a JSON object, and the methods in `Allow`.
- **Defaults that track the outside world:** the recommended Docker Engine version, Go's TLS defaults, and the MCP
  protocol revisions offered. Raising the [minimum Docker Engine version][requirements] ships in a minor release,
  marked **Breaking**.
- **Fixes toward a specification.** When a header, a status code, or a grammar contradicts a specification Cetacean
  cites, the fix ships in a minor release, marked **Breaking**. Security fixes do too.

### Deprecation

A deprecated route, setting, tool, argument or field keeps working for at least two minor releases, and is
removed only in the next major release. The changelog lists it under **Deprecated** in the release that deprecates
it.

- **Routes** answer with a `Deprecation` header ([RFC 9745](https://www.rfc-editor.org/rfc/rfc9745)), a `Sunset`
  header ([RFC 8594](https://www.rfc-editor.org/rfc/rfc8594)) once the removal date is known, and
  `Link: <…>; rel="deprecation"` pointing at the changelog entry. The OpenAPI spec marks them `deprecated: true`.
  Once removed, a route answers `410 Gone` with code `API012`.
- **MCP tools** say so in the first sentence of their description and in
  `_meta["io.cetacean/deprecated"]`, which names the release and the replacement:
  `{"since": "1.3.0", "replacement": "find"}`. A deprecated argument is still accepted, and documented as such.
- **Settings** are still read, log one warning at startup naming their replacement, and are marked in the
  [configuration reference][configuration]. Setting both a deprecated setting and its replacement stops Cetacean at
  startup instead of silently picking one.

[api-explorer]: api/explorer
[api-tokens]: authentication#api-access-tokens
[auth.mode]: configuration#auth.mode
[authentication]: authentication
[authorization]: authorization
[configuration]: configuration
[dashboard]: dashboard
[mcp]: mcp
[mcp-tools]: mcp-tools
[mcp-tools.compatibility]: mcp-tools#compatibility
[mcp.enabled]: configuration#mcp.enabled
[oauth.api_tokens]: configuration#oauth.api_tokens
[oauth.enabled]: configuration#oauth.enabled
[operations-level]: configuration#operations-level
[recommendations]: recommendations
[requirements]: getting-started#requirements
[server.allowed_hosts]: configuration#server.allowed_hosts
[server.cors.origins]: configuration#server.cors.origins
[server.public_url]: configuration#server.public_url
[server.sse.batch_interval]: configuration#server.sse.batch_interval
[server.trusted_proxies]: configuration#server.trusted_proxies
[storage.data_dir]: configuration#storage.data_dir
