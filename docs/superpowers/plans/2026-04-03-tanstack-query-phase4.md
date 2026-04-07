# TanStack Query Migration — Phase 4: List Pages (`useInfiniteQuery`)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `useSwarmResource` with a TanStack Query-backed `useSwarmQuery` wrapper that uses `useInfiniteQuery` for Range pagination + SSE optimistic cache mutations.

**Architecture:** Create a new `useSwarmQuery` hook wrapping `useInfiniteQuery`. SSE events from `useResourceStream` dispatch to `queryClient.setQueryData()` for optimistic in-place updates (matching current behavior) or `invalidateQueries()` for sync events. The hook preserves the exact return shape of `useSwarmResource` so consuming pages and `ResourceListPage` need minimal changes. Then swap all consumers to use the new hook.

**Tech Stack:** `@tanstack/react-query` v5, React 19, TypeScript

---

## File Structure

- **Create:** `frontend/src/hooks/useSwarmQuery.ts` — new hook wrapping `useInfiniteQuery` + SSE
- **Create:** `frontend/src/hooks/useSwarmQuery.test.tsx` — tests for the new hook
- **Modify:** `frontend/src/components/ResourceListPage.tsx` — swap `useSwarmResource` → `useSwarmQuery`
- **Modify:** `frontend/src/pages/NodeList.tsx` — swap import + call
- **Modify:** `frontend/src/pages/ServiceList.tsx` — swap import + call
- **Modify:** `frontend/src/pages/TaskList.tsx` — swap import + call
- **Delete:** `frontend/src/hooks/useSwarmResource.ts` — replaced by `useSwarmQuery`
- **Delete:** `frontend/src/hooks/useSwarmResource.test.tsx` — replaced by `useSwarmQuery.test.tsx`

---

### Task 1: Create `useSwarmQuery` Hook

**Files:**
- Create: `frontend/src/hooks/useSwarmQuery.ts`

This is the core of Phase 4. The hook wraps `useInfiniteQuery` and `useResourceStream` to provide the same API surface as `useSwarmResource`.

- [ ] **Step 1: Implement the hook**

Create `frontend/src/hooks/useSwarmQuery.ts`:

```typescript
import { emptyMethods, pageSize, setsEqual } from "../api/client";
import type { FetchResult } from "../api/client";
import type { CollectionResponse } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";

const ssePathMap: Record<string, string> = {
  node: "/nodes",
  service: "/services",
  task: "/tasks",
  config: "/configs",
  secret: "/secrets",
  network: "/networks",
  volume: "/volumes",
  stack: "/stacks",
};

export function useSwarmQuery<T>(
  queryKey: readonly unknown[],
  fetchFn: (offset: number, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>,
  sseType: string,
  getId: (item: T) => string,
) {
  const queryClient = useQueryClient();
  const getIdRef = useRef(getId);
  getIdRef.current = getId;
  const [allowedMethods, setAllowedMethods] = useState<Set<string>>(emptyMethods);

  const query = useInfiniteQuery({
    queryKey: [...queryKey],
    queryFn: async ({ pageParam, signal }) => {
      const result = await fetchFn(pageParam, signal);

      // Update allowedMethods from the latest response.
      setAllowedMethods((previous) =>
        setsEqual(previous, result.allowedMethods) ? previous : result.allowedMethods,
      );

      return result.data;
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage) => {
      const nextOffset = lastPage.offset + lastPage.items.length;
      return nextOffset < lastPage.total ? nextOffset : undefined;
    },
  });

  // Flat data array from all pages.
  const data = query.data?.pages.flatMap((page) => page.items) ?? [];

  // Total from the most recent page (freshest server value).
  const lastPage = query.data?.pages[query.data.pages.length - 1];
  const total = lastPage?.total ?? 0;

  // SSE optimistic cache mutations.
  const ssePath = ssePathMap[sseType] ?? `/events?types=${sseType}`;

  useResourceStream(
    ssePath,
    useCallback(
      (event) => {
        if (event.type === "sync") {
          queryClient.invalidateQueries({ queryKey: [...queryKey] });
          return;
        }

        const currentPages = query.data?.pages;
        if (!currentPages) {
          return;
        }

        if (event.action === "remove") {
          queryClient.setQueryData(
            [...queryKey],
            (old: typeof query.data) => {
              if (!old) {
                return old;
              }

              return {
                ...old,
                pages: old.pages.map((page) => {
                  const filtered = page.items.filter(
                    (item) => getIdRef.current(item) !== event.id,
                  );

                  if (filtered.length === page.items.length) {
                    return page;
                  }

                  return {
                    ...page,
                    items: filtered,
                    total: page.total - 1,
                  };
                }),
              };
            },
          );
        } else if (event.resource) {
          const resource = event.resource as T;
          let found = false;

          // Check if item exists in any loaded page.
          for (const page of currentPages) {
            if (page.items.some((item) => getIdRef.current(item) === event.id)) {
              found = true;
              break;
            }
          }

          if (found) {
            // Update in-place.
            queryClient.setQueryData(
              [...queryKey],
              (old: typeof query.data) => {
                if (!old) {
                  return old;
                }

                return {
                  ...old,
                  pages: old.pages.map((page) => ({
                    ...page,
                    items: page.items.map((item) =>
                      getIdRef.current(item) === event.id ? resource : item,
                    ),
                  })),
                };
              },
            );
          } else {
            // Unknown item: bump total on all pages so hasMore updates.
            queryClient.setQueryData(
              [...queryKey],
              (old: typeof query.data) => {
                if (!old) {
                  return old;
                }

                return {
                  ...old,
                  pages: old.pages.map((page) => ({
                    ...page,
                    total: page.total + 1,
                  })),
                };
              },
            );
          }
        } else {
          // Event without resource payload — invalidate.
          queryClient.invalidateQueries({ queryKey: [...queryKey] });
        }
      },
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [queryClient, query.data?.pages],
    ),
  );

  const loading = query.isLoading;
  const loadingMore = query.isFetchingNextPage;
  const error = query.error ?? null;
  const hasMore = query.hasNextPage ?? false;

  const loadMore = useCallback(() => {
    if (!query.isFetchingNextPage && query.hasNextPage) {
      query.fetchNextPage();
    }
  }, [query]);

  const retry = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: [...queryKey] });
  }, [queryClient, queryKey]);

  return { data, total, loading, loadingMore, error, retry, hasMore, loadMore, allowedMethods };
}
```

Key design decisions:
- `useInfiniteQuery` manages page accumulation — the `pages` Map, `nextPageRef`, `sseOffset`, `loadingMoreRef` all disappear
- `getNextPageParam` uses `offset + items.length < total` to decide if there's a next page
- SSE `remove`: filters item from pages and decrements `total` on all pages
- SSE `update` (known): replaces item in-place in the correct page
- SSE `update` (unknown): increments `total` on all pages (so `hasNextPage` updates)
- SSE `sync` or no-payload: full invalidation
- `allowedMethods` stored in component state, updated from each fetch response
- `queryKey` is spread to avoid referential instability from the array

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean (hook is not yet consumed).

- [ ] **Step 3: Commit**

```bash
git add src/hooks/useSwarmQuery.ts
git commit -m "feat(frontend): add useSwarmQuery hook wrapping useInfiniteQuery + SSE"
```

---

### Task 2: Write Tests for `useSwarmQuery`

**Files:**
- Create: `frontend/src/hooks/useSwarmQuery.test.tsx`

Port the tests from `useSwarmResource.test.tsx` to test `useSwarmQuery`. The test setup needs `QueryClientProvider` wrapping.

- [ ] **Step 1: Write the test file**

Create `frontend/src/hooks/useSwarmQuery.test.tsx`. Tests should cover:

1. **Fetches initial data** — mock fetchFn returns first page, verify `data`, `total`, `loading` states.
2. **Exposes loadMore and hasMore** — mock fetchFn returns page 0 (2 items, total 3) then page 1 (1 item). Verify `hasMore` starts true, `loadMore()` fetches next page, data accumulates, `hasMore` becomes false.
3. **Handles fetch errors** — mock fetchFn rejects, verify `error` is set.
4. **SSE updates item in-place** — initial fetch, then simulate SSE update event for a loaded item. Verify data is updated.
5. **SSE bumps total for unknown items** — initial fetch with partial data (total > loaded count). SSE event for unknown ID. Verify total increases.
6. **SSE removes item** — initial fetch with 2 items. SSE remove event. Verify data has 1 item and total decremented.
7. **Retry invalidates queries** — fetch error, call retry, verify refetch.

Use the same `MockEventSource` pattern from the existing tests. The wrapper must include `QueryClientProvider` with a fresh `QueryClient` per test (`retry: false, gcTime: 0`).

The mock `fetchFn` signature is `(offset: number, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>`. Return `{ data: { items, total, limit: 50, offset }, allowedMethods: new Set(["GET", "HEAD"]) }`.

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/hooks/useSwarmQuery.test.tsx`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add src/hooks/useSwarmQuery.test.tsx
git commit -m "test(frontend): add useSwarmQuery tests"
```

---

### Task 3: Swap All Consumers to `useSwarmQuery`

**Files:**
- Modify: `frontend/src/components/ResourceListPage.tsx`
- Modify: `frontend/src/pages/NodeList.tsx`
- Modify: `frontend/src/pages/ServiceList.tsx`
- Modify: `frontend/src/pages/TaskList.tsx`

The return shape of `useSwarmQuery` matches `useSwarmResource` exactly: `{ data, total, loading, loadingMore, error, retry, hasMore, loadMore, allowedMethods }`. The call signature changes slightly:
- Old: `useSwarmResource(fetchFn, sseType, getId)`
- New: `useSwarmQuery(queryKey, fetchFn, sseType, getId)`

Each consumer needs:
1. Change import from `useSwarmResource` to `useSwarmQuery`
2. Add a `queryKey` as the first argument
3. The `fetchFn` signature stays the same: `(offset: number, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>`

- [ ] **Step 1: Update `ResourceListPage.tsx`**

Change import and add query key:

```typescript
import { useSwarmQuery } from "../hooks/useSwarmQuery";

// In the component:
const { data, loading, error, retry, hasMore, loadMore, allowedMethods } = useSwarmQuery(
  [config.path, { search: debouncedSearch, sort: sortKey, dir: sortDir }],
  useCallback(
    (offset: number, signal: AbortSignal) =>
      config.fetchFn({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }, signal),
    [debouncedSearch, sortKey, sortDir, config.fetchFn],
  ),
  config.sseType,
  config.keyFn,
);
```

The `config.path` (e.g., `"/configs"`, `"/secrets"`) is used as the query key prefix.

- [ ] **Step 2: Update `NodeList.tsx`**

Change import and add query key:

```typescript
import { useSwarmQuery } from "../hooks/useSwarmQuery";

const { data: nodes, loading, error, retry, hasMore, loadMore } = useSwarmQuery(
  ["nodes", { search: debouncedSearch, sort: sortKey, dir: sortDir }],
  useCallback(
    (offset: number, signal: AbortSignal) =>
      api.nodes({ search: debouncedSearch, sort: sortKey, dir: sortDir, offset }, signal),
    [debouncedSearch, sortKey, sortDir],
  ),
  "node",
  ({ ID }: Node) => ID,
);
```

- [ ] **Step 3: Update `ServiceList.tsx`**

Same pattern with `["services", { search, sort, dir }]` query key.

- [ ] **Step 4: Update `TaskList.tsx`**

Same pattern with `["tasks", { search, sort, dir, filter }]` query key (TaskList may have a filter parameter).

- [ ] **Step 5: Update existing test files**

Tests that mock `useSwarmResource` need to mock `useSwarmQuery` instead. Update:
- `frontend/src/pages/NodeList.test.tsx`
- `frontend/src/pages/ServiceList.test.tsx`
- `frontend/src/pages/ConfigList.test.tsx`
- `frontend/src/pages/SecretList.test.tsx`
- `frontend/src/pages/NetworkList.test.tsx`
- `frontend/src/pages/VolumeList.test.tsx`
- Any test that imports from `../hooks/useSwarmResource`

The mock return shape is the same — just change the module path.

- [ ] **Step 6: Run TypeScript check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 7: Run all tests**

Run: `npx vitest run`
Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add src/components/ResourceListPage.tsx src/pages/NodeList.tsx src/pages/ServiceList.tsx src/pages/TaskList.tsx src/pages/NodeList.test.tsx src/pages/ServiceList.test.tsx src/pages/ConfigList.test.tsx src/pages/SecretList.test.tsx src/pages/NetworkList.test.tsx src/pages/VolumeList.test.tsx
git commit -m "refactor(frontend): swap all list pages to useSwarmQuery"
```

---

### Task 4: Remove Old `useSwarmResource`

**Files:**
- Delete: `frontend/src/hooks/useSwarmResource.ts`
- Delete: `frontend/src/hooks/useSwarmResource.test.tsx`
- Modify: `frontend/src/pages/StackList.tsx` (if it imports useSwarmResource)

- [ ] **Step 1: Check for any remaining imports**

Search for `useSwarmResource` across the entire `frontend/src/` tree. Remove any remaining references.

- [ ] **Step 2: Delete the old files**

```bash
rm frontend/src/hooks/useSwarmResource.ts
rm frontend/src/hooks/useSwarmResource.test.tsx
```

- [ ] **Step 3: Verify compilation and tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: Clean, all pass.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor(frontend): remove old useSwarmResource (replaced by useSwarmQuery)"
```

---

### Task 5: Full Verification

**Files:** None new.

- [ ] **Step 1: TypeScript check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 2: All frontend tests**

Run: `npx vitest run`
Expected: All pass.

- [ ] **Step 3: Lint and format**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint && make fmt-check`
Expected: Clean. If not, run `make fmt` first.

- [ ] **Step 4: Build**

Run: `make build`
Expected: Builds successfully.
