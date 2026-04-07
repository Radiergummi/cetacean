# TanStack Query Migration Design

## Overview

Replace all hand-rolled data fetching hooks with TanStack Query v5. The migration is incremental (hook by hook), preserving the same API surface for page components via wrapper hooks. SSE integration uses optimistic cache mutation for loaded data and invalidation for sync events.

## Motivation

The frontend has 24 distinct data-fetching patterns, each manually managing loading/error/data state, abort controllers, caching, retry, and background updates. `useSwarmResource` alone is ~200 lines of subtle state management for page accumulation, SSE offset tracking, and concurrent load guards. TanStack Query handles all of this out of the box, with `useInfiniteQuery` purpose-built for the Range pagination pattern.

## Installation and Provider Setup

Install `@tanstack/react-query` and `@tanstack/react-query-devtools`.

Wrap the app in `QueryClientProvider` at the root, alongside existing providers.

Default `QueryClient` config:
- `staleTime: 0` — data always considered stale, served from cache while refetching
- `gcTime: 300_000` (5 minutes) — keep unused queries in cache
- `retry: 1` — one retry on failure
- `refetchOnWindowFocus: false` — SSE handles liveness

Devtools included only in dev builds via `import.meta.env.DEV`.

## Query Key Convention

```typescript
// List queries (infinite)
["nodes", { search, sort, dir }]
["services", { search, sort, dir, filter }]
["tasks", { search, sort, dir, filter }]

// Detail queries
["node", nodeId]
["service", serviceId]
["config", configId]

// Singleton queries
["cluster"]
["monitoring-status"]
["recommendations"]
["topology"]

// Metrics
["metrics", { query, start, end, step }]
["cluster-metrics"]

// History
["history", { type, resourceId, limit }]
```

List keys include filter/sort params as an object — changing params produces a new cache entry. Switching back to a previous combination is instant from cache.

SSE invalidation uses prefix matching: `queryClient.invalidateQueries({ queryKey: ["services"] })` invalidates all service list queries regardless of filter params.

## Migration Order

Leaf dependencies first, most complex last:

1. **Singleton queries** — `useMonitoringStatus`, `useRecommendations`, cluster overview, search, plugins, disk usage. Simple `useQuery` replacements, no SSE, no pagination.
2. **Metrics hooks** — `useInstanceResolver`, `useNodeMetrics`, `useServiceMetrics`, `useTaskMetrics`, `CapacitySection`, `NodeResourceGauges`. Replace `setInterval` with `refetchInterval`.
3. **Detail pages** — `useDetailResource` → `useQuery` + SSE invalidation. 4 consuming pages.
4. **List pages** — `useSwarmResource` → `useInfiniteQuery` + SSE optimistic updates. The big one.
5. **Special cases** — Topology, MetricsConsole, TimeSeriesChart, log viewer (last, may stay custom if migration doesn't simplify it).

Each step is independently committable and testable. Old and new patterns coexist during migration.

## Wrapper Hooks

Page components don't use TanStack Query directly. Wrapper hooks preserve the same return type:

### `useSwarmQuery`

Wraps `useInfiniteQuery` + SSE subscription. Same API surface as today's `useSwarmResource`:

```typescript
function useSwarmQuery<T>(
  queryKey: QueryKey,
  fetchFn: (offset: number, signal: AbortSignal) => Promise<CollectionResponse<T>>,
  sseType: string,
  getId: (item: T) => string,
): {
  data: T[];
  total: number;
  loading: boolean;
  loadingMore: boolean;
  error: Error | null;
  retry: () => void;
  hasMore: boolean;
  loadMore: () => void;
  allowedMethods: Set<string>;
}
```

Page components swap `useSwarmResource` for `useSwarmQuery` with no template changes.

### `useDetailQuery`

Wraps `useQuery` + SSE-driven invalidation. Same API surface as today's `useDetailResource`:

```typescript
function useDetailQuery<T>(
  queryKey: QueryKey,
  fetchFn: (key: string, signal: AbortSignal) => Promise<T>,
  ssePath: string,
): {
  data: T | null;
  history: HistoryEntry[];
  error: Error | null;
  retry: () => void;
  allowedMethods: Set<string>;
}
```

## `useInfiniteQuery` for Range Pagination

```typescript
useInfiniteQuery({
  queryKey: ["services", { search, sort, dir }],
  queryFn: async ({ pageParam, signal }) => {
    return fetchRange<Service>("/services", {
      offset: pageParam, search, sort, dir,
    }, signal);
  },
  initialPageParam: 0,
  getNextPageParam: (lastPage) => {
    const nextOffset = lastPage.offset + lastPage.items.length;
    return nextOffset < lastPage.total ? nextOffset : undefined;
  },
});
```

TanStack Query manages page accumulation natively:
- `data.pages` is the array of page responses
- `data.pages.flatMap(p => p.items)` is the flat item list
- `fetchNextPage()` is what the DataTable sentinel calls
- `hasNextPage` replaces manual `hasMore`
- `isFetchingNextPage` replaces `loadingMore`

The entire `pages` Map, `nextPageRef`, `sseOffset`, `loadingMoreRef`, and `loadPage` callback disappear (~100 lines of state management replaced by configuration).

Changing search/sort/dir changes the query key → TanStack Query treats it as a new query → pages reset automatically.

## Response Metadata (`allowedMethods`)

The `queryFn` returns the full response including `allowedMethods`. The cached value is the wrapper object. The wrapper hook reads `allowedMethods` from the last fetched page and exposes it as a top-level return value.

This is the idiomatic TanStack Query v5 pattern: `queryFn` returns the complete response, the wrapper hook projects what consumers need. No `meta` (which is static and per-key, not per-fetch), no `onSuccess` (removed in v5).

## SSE Integration

`useResourceStream` stays as the SSE transport layer. What changes is the callback:

### List queries (inside `useSwarmQuery`)

| SSE Event | Action |
|-----------|--------|
| `update` with payload, item in cached pages | `setQueryData` — patch item in-place within its page |
| `update` with payload, item not in cached pages | `setQueryData` — increment total in the last page |
| `remove`, item in cached pages | `setQueryData` — remove from its page, decrement total |
| `remove`, item not in cached pages | `setQueryData` — decrement total only |
| `sync` | `invalidateQueries` — background refetch of all pages |
| Event without payload | `invalidateQueries` — same as sync |

Same logic as today's `useSwarmResource`, expressed through `setQueryData` instead of manual state setters.

### Detail queries (inside `useDetailQuery`)

SSE events trigger `invalidateQueries({ queryKey })` with a 500ms debounce. The query refetches in the background while stale data stays visible.

### Topology

SSE events trigger `invalidateQueries({ queryKey: ["topology"] })` with a 2-second debounce.

## What Doesn't Change

- **API client** (`client.ts`) — `fetchRange`, `fetchJSON`, `fetchJGF`, all API methods unchanged
- **`useResourceStream`** — SSE transport unchanged, only callbacks change
- **`DataTable`** — receives `data`, `hasMore`, `onLoadMore` as today
- **Page component JSX** — templates unchanged, wrapper hooks match current return types
- **`useAuth`** — stays as context provider
- **Log viewer** — cursor-based pagination with live streaming is the most complex pattern. Last in migration order; may stay custom if TanStack Query integration doesn't simplify it.
- **Backend** — zero changes
