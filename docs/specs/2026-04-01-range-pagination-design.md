# Range Request Pagination Design

## Overview

Replace query-param-based pagination (`?limit=&offset=`) with HTTP Range Requests using a custom `items` range unit, served as a progressive enhancement. The frontend switches to Range-only pagination with infinite scroll in DataTable.

## Motivation

HTTP Range Requests map naturally to collection pagination: `Range: items 0-24` requests the first 25 items, `Content-Range: items 0-24/142` communicates the window and total. This is semantically cleaner than query param conventions, uses standard HTTP headers, and keeps resource URLs free of pagination parameters.

The existing query param path is preserved as a fallback for external clients, making this a progressive enhancement.

## Protocol Behavior

### Request Parsing Priority

1. If `?limit=` or `?offset=` query params are present, use them. Range header is ignored. Response is `200`.
2. Otherwise, if `Range: items <start>-<end>` header is present, use it. Response is `206` (or `200`/`416`, see below).
3. Otherwise, use defaults (`limit=50, offset=0`). Response is `200`.

### Range Header Format

```
Range: items <start>-<end>
```

- `start` and `end` are zero-based, inclusive. `items 0-24` means 25 items starting at offset 0.
- Non-`items` units (e.g., `bytes`) are ignored (fall through to defaults).
- Multipart ranges (`items 0-9, 50-59`) are rejected with `416 Range Not Satisfiable`.
- Max limit of 200 applies: `items 0-999` is clamped to `items 0-199`.

### Response Status Codes

| Scenario | Status | Key Headers |
|----------|--------|-------------|
| Range used, items in range | `206 Partial Content` | `Content-Range: items 0-24/142` |
| Range used, covers entire collection | `200 OK` | `Accept-Ranges: items` |
| Range used, offset beyond total | `416 Range Not Satisfiable` | `Content-Range: items */142` |
| Range used, empty collection (total=0) | `200 OK` | `Accept-Ranges: items` |
| Query params used or no pagination | `200 OK` | `Accept-Ranges: items`, Link headers |

`Accept-Ranges: items` is sent on every collection response regardless of which path was used.

When the range covers the full collection (e.g., `items 0-49` with total=30), the response is `200` not `206` since there is nothing partial about it.

### Interaction with Other Query Params

Sort (`?sort=&dir=`), search (`?search=`), and filter (`?filter=`) remain as query params. They are orthogonal to Range: the server applies filtering/sorting first, then slices the result per the Range header.

A typical request:
```
GET /services?sort=name&dir=asc&search=web
Range: items 0-49
Accept: application/json
```

### ETag and Conditional Requests

ETag and `304 Not Modified` behavior is unchanged. Works on both `200` and `206` responses.

## Backend Changes

### `pagination.go`

**`PageParams` struct** gains a `RangeReq bool` field indicating whether pagination came from a Range header.

**`parsePagination` expansion:**

1. Check for `?limit=` or `?offset=` query params. If present: use them, `RangeReq=false`.
2. Check `Range` header. Parse `items <start>-<end>`.
   - Multipart (contains `,`): return error, handler writes `416`.
   - Non-`items` unit: ignore, fall through to step 3.
   - Valid single range: `Offset=start`, `Limit=end-start+1` (capped at 200), `RangeReq=true`.
3. Defaults: `limit=50, offset=0, RangeReq=false`.

**New `writeCollectionResponse` function** replaces the current `writePaginationLinks` + `writeCachedJSON` two-step in handlers. It encapsulates all response logic:

- Always sets `Accept-Ranges: items`.
- If `RangeReq=true`:
  - Total is 0: `200` with empty collection.
  - Range covers entire collection: `200`.
  - Offset beyond total: `416` with `Content-Range: items */TOTAL`.
  - Otherwise: `206` with `Content-Range: items START-END/TOTAL`.
- If `RangeReq=false`:
  - `200` with Link headers as today.

Response body is always `CollectionResponse` (with `@context`, `@type`, `items`, `total`, `limit`, `offset`) for all success cases. No body on `416`.

### Handler Changes

Each list handler currently calls:
```go
p := parsePagination(r)
sorted := sortItems(items, p.Sort, p.Dir, accessors)
resp := applyPagination(ctx, sorted, p)
writePaginationLinks(w, r, resp.Total, resp.Limit, resp.Offset)
writeCachedJSON(w, r, resp)
```

This becomes:
```go
p, err := parsePagination(r)
if err != nil {
    // 416 for multipart ranges
}
sorted := sortItems(items, p.Sort, p.Dir, accessors)
resp := applyPagination(ctx, sorted, p)
writeCollectionResponse(w, r, resp, p)
```

Affected handlers: nodes, services, tasks, configs, secrets, networks, volumes, stacks.

## Frontend Changes

### API Client (`client.ts`)

New `fetchRange` helper:

```typescript
async function fetchRange<T>(
    path: string,
    offset: number,
    limit: number,
): Promise<CollectionResponse<T>> {
    const response = await fetch(path, {
        headers: {
            "Accept": "application/json",
            "Range": `items ${offset}-${offset + limit - 1}`,
        },
    });
    if (!response.ok && response.status !== 206) throw ...;
    return response.json();
}
```

List methods (`api.nodes()`, `api.services()`, etc.) switch to `fetchRange`. Sort, search, and filter stay as query params on the URL path. `ListParams` drops `limit`/`offset`, and `buildListURL` only builds the non-pagination query string.

### `useSwarmResource` Hook

Changes from single-fetch to paged accumulation:

- `pages: Map<number, T[]>` keyed by page number.
- `flatItems` computed from pages in order, fed to DataTable.
- `total` from the most recent `CollectionResponse`.
- `loadPage(pageNumber)` fetches a page via `fetchRange`, appends to `pages`.
- Exposes `loadMore()` callback for the next unloaded page and `hasMore` boolean.
- Page size: 50 items (matches current default).

**Reset on param change:** When sort/search/filter params change, pages are cleared and page 0 is fetched fresh.

**SSE handling:** Unchanged strategy, adapted for partial data:
- SSE event references a loaded item: update in-place.
- SSE event references an unknown item (not yet loaded): bump `total` count.
- SSE event removes a loaded item: remove from pages, decrement total.
- `sync` event: full reset, refetch from page 0.

### `DataTable` Component

New props:
- `onLoadMore?: () => void` — called when the user scrolls near the end of loaded data.
- `hasMore?: boolean` — whether more pages are available.

The virtualizer watches the last visible row index. When it comes within 10 rows of the end of loaded data and `hasMore` is true, it calls `onLoadMore`. A loading indicator renders at the bottom while a fetch is in flight.

**Guard against rapid scrolling:** A `loadingPage` flag prevents concurrent fetches. Pages load sequentially.

## Edge Cases

**416 on page fetch:** If a page fetch returns `416` (items deleted between pages), treat as end-of-list: `hasMore=false`, no retry.

**Stale total:** The locally-tracked total (server total + SSE offset) drives "has more" decisions, not the stale value from the last response. This avoids premature end-of-list and infinite loading.

**Empty collection:** `Range: items 0-49` against an empty collection returns `200` with `items: [], total: 0`. Hook sets `hasMore=false`, shows `EmptyState`.

## What Doesn't Change

- Detail endpoints (single resource, no pagination)
- SSE endpoints (streaming)
- Search endpoint (`/search?q=`) with its per-type limit semantic
- Meta endpoints (`/-/health`, `/-/ready`, etc.)
- `CollectionResponse` struct fields and JSON-LD metadata
- Link headers on the query-param `200` path
- ACL filtering and expression filtering (applied before pagination)
- ETag / conditional 304

## What Needs Updating

- OpenAPI spec (`api/openapi.yaml`): document `Range` header, `206` responses, `Accept-Ranges` and `Content-Range` headers on all list endpoints.
- Pagination tests: add Range header parsing, response status/header assertions.
- Frontend tests: `useSwarmResource` infinite scroll behavior.
