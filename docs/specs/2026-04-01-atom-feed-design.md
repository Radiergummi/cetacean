# Atom Feed Support

**Date:** 2026-04-01
**Status:** Draft

## Summary

Add Atom (RFC 4287) feed support to Cetacean as a first-class content negotiation option. Clients request feeds via `Accept: application/atom+xml` header or `.atom` URL suffix on existing resource endpoints. This provides a standards-based alternative to SSE for consuming cluster change events, allowing integration with feed readers, monitoring tools, and automation pipelines.

## Motivation

SSE is Cetacean's real-time update mechanism, but it requires a persistent connection and custom client code. Atom feeds offer a pull-based, RFC-standardized interface that works with existing tooling out of the box — feed readers, webhook pipelines, CLI tools like `curl`, and any system that speaks HTTP + XML.

## Design

### Content Negotiation

The `negotiate` middleware gains a new `ContentTypeAtom` constant. Resolution:

1. `.atom` URL suffix (highest priority, like `.json`/`.html` today)
2. `Accept: application/atom+xml` header
3. Existing types unchanged

The `supportedTypes` table gets a new entry: `{"application", "atom+xml", ContentTypeAtom}`.

Note: Adding `application/atom+xml` to `supportedTypes` means any endpoint receiving that Accept header will get `ContentTypeAtom` in context. Meta endpoints (`/-/*`, `/api/*`) bypass content negotiation entirely (they don't use dispatch helpers), so this has no effect on them.

### Dispatch Helpers

Both dispatch helpers gain an `atomHandler` parameter:

```go
func contentNegotiated(jsonHandler, atomHandler http.HandlerFunc, spa http.Handler) http.HandlerFunc
func contentNegotiatedWithSSE(jsonHandler, sseHandler, atomHandler http.HandlerFunc, spa http.Handler) http.HandlerFunc
```

Note: `sseOnly` was removed from the codebase and is not affected.

A `nil` atom handler causes the dispatch helper to return 406 Not Acceptable for Atom requests. This is the mechanism for endpoints where feeds don't apply.

### Atom XML Package

A new `internal/api/atom/` package defines Atom structs per RFC 4287:

- **`Feed`** — `<feed>` with `<title>`, `<id>`, `<updated>`, `<link>` (self, alternate, next/prev per RFC 5005), `<entry>` list
- **`Entry`** — `<entry>` with `<id>`, `<title>`, `<updated>`, `<link rel="alternate">`, `<content type="text">`, `<category term="..."/>`

**Feed `<id>`:** Tag URI (`tag:hostname,YYYY:path`) derived from the request `Host` header (or `X-Forwarded-Host` behind a reverse proxy) and the request path. Operators behind a reverse proxy should ensure a consistent `Host` header for stable feed IDs.

**Entry `<content>`:** The `Summary` field from `HistoryEntry`. When `Summary` is empty, falls back to `"{Action} {Name}"` (e.g., "update myservice").

**Feed `<updated>`:** Timestamp of the newest entry. For empty feeds (no matching history entries), uses the current time.

Rendering via `encoding/xml` with struct tags. A `RenderFeed(w, feed)` function writes `application/atom+xml;charset=utf-8` with XML declaration and Atom namespace.

### Atom Handlers

A new `atom_handlers.go` file contains atom handler functions. Three patterns:

**List endpoints** (`/nodes`, `/services`, etc.) — Query History filtered by `EventType`. Each `HistoryEntry` becomes an `<entry>`. `<link rel="alternate">` points to the resource detail URL. Feed `<title>`: "Cetacean — Services".

**Detail endpoints** (`/nodes/{id}`, `/services/{id}`, etc.) — Query History filtered by `EventType` + `ResourceID`. Feed `<title>` includes resource name.

**Global endpoints** (`/events`, `/history`) — Query History with no type filter. `<category>` element distinguishes resource types.

**Recommendations** (`/recommendations`) — Queries the recommendation engine's current results. Each recommendation maps to an `<entry>` with category, severity, and description. Entry `<id>` derived from a stable hash of the recommendation (resource + check type) to avoid duplicates across polls. Entry `<updated>` is the engine's last evaluation timestamp. `<link rel="alternate">` points to `/recommendations` (no individual detail pages exist).

**Search** (`/search?q=`) — Searches history entries whose `Name` matches the query string. This differs from the JSON search endpoint (which searches current resources) — the Atom feed surfaces historical changes matching the query.

A shared `buildFeed` helper takes a `HistoryQuery` + request context and returns an `atom.Feed`.

### Pagination

Cursor-based pagination using `HistoryEntry.ID` (monotonically increasing). Query parameters: `?before=<id>&limit=50`. This is stable for a live event ring buffer — new entries don't shift existing cursors. Default limit is 50.

This requires adding a `BeforeID uint64` field to `HistoryQuery` and updating `History.List` to start iteration from the entry preceding the given ID rather than always from the newest.

RFC 5005 (Feed Paging and Archiving) `<link rel="next">` element in the feed, with `href` containing `?before=<oldest-entry-id>&limit=<limit>`. `<link rel="previous">` when applicable (entries exist newer than the first entry in the current page).

### ACL Filtering

History entries are filtered through `acl.Filter()` before rendering — entries for resources the user can't read are excluded. Different users requesting the same feed URL may receive different content.

The `Vary` header is extended to include the auth-relevant header: `Vary: Accept, Authorization` for Bearer token auth, `Vary: Accept, Cookie` for OIDC browser sessions. This is composed by the auth middleware alongside the existing `Vary: Accept` set by the negotiate middleware.

### ETag Support

A `writeAtomWithETag` helper computes a SHA-256 ETag (truncated to 16 bytes) over the rendered XML and handles `If-None-Match` / 304 responses, reusing the existing `etagMatch` function. Useful for polling clients.

Responses include `Cache-Control: no-cache` to ensure revalidation on each request while still allowing conditional requests.

### Endpoint Coverage

**Gets Atom support:**
- All resource list endpoints: `/nodes`, `/services`, `/tasks`, `/stacks`, `/configs`, `/secrets`, `/networks`, `/volumes`, `/plugins`
- All resource detail endpoints: `/nodes/{id}`, `/services/{id}`, `/tasks/{id}`, `/stacks/{name}`, `/configs/{id}`, `/secrets/{id}`, `/networks/{id}`, `/volumes/{name}`, `/plugins/{name}`
- `/events`, `/history`
- `/search` — feed of history entries matching the search query by name
- `/recommendations` — feed of current recommendations

**Does not get Atom (nil handler, returns 406):**
- `/cluster`, `/cluster/metrics`, `/cluster/capacity` — point-in-time aggregates
- `/swarm` — cluster config snapshot
- `/disk-usage` — point-in-time snapshot
- `/topology/networks`, `/topology/placement` — derived views
- `/metrics` — Prometheus proxy
- `/stacks/summary` — aggregate stats

**Not applicable:**
- Meta endpoints (`/-/*`, `/api/*`) — bypass content negotiation entirely
- Write endpoints (`PUT`, `PATCH`, `POST`, `DELETE`) — not GET
- Log-tail endpoints — streaming logs, not event feeds
- Sub-resource GETs (`/services/{id}/env`, `/nodes/{id}/labels`, etc.) — current state, not history

## Testing

**Unit tests:**
- `atom/` package: XML output validation, required elements, empty feed handling, pagination links
- `negotiate`: `.atom` suffix stripping, `Accept: application/atom+xml` parsing including quality values and wildcards
- Dispatch: nil atom handler returns 406, non-nil routes correctly (all three dispatch variants)
- `buildFeed`: history-to-entry mapping, ACL filtering, cursor-based pagination, empty summary fallback
- `HistoryQuery.BeforeID`: cursor-based iteration correctness

**Integration tests:**
- Request resource endpoints with `Accept: application/atom+xml` and `.atom` suffix, verify valid Atom XML
- Non-Atom endpoint (e.g., `/cluster`) returns 406
- Mixed Accept header quality negotiation (Atom vs JSON)
- ETag/304 round-trip
- Cursor pagination: verify `<link rel="next">` contains correct `before` parameter, follow it, verify stable results

No frontend changes required.
