# TanStack Query Migration — Phase 3: Detail Pages

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate `useDetailResource` from manual fetch + SSE debounce to TanStack Query's `useQuery` + `invalidateQueries`, covering the 4 detail pages that consume it (ConfigDetail, NetworkDetail, VolumeDetail, SecretDetail).

**Architecture:** Replace `useDetailResource` internals with two `useQuery` calls (resource + history) and SSE-driven invalidation via `useQueryClient().invalidateQueries()`. The hook's return shape stays identical — consuming pages need zero changes.

**Tech Stack:** `@tanstack/react-query` v5, React 19, TypeScript

---

## Scope

Only `useDetailResource` and its 4 consumers. The more complex detail pages (NodeDetail, ServiceDetail, TaskDetail) have custom multi-resource fetch + SSE patterns that warrant their own phase.

## File Structure

- **Modify:** `frontend/src/hooks/useDetailResource.ts` — replace internals with useQuery
- Pages are NOT modified (return shape is preserved)

---

### Task 1: Migrate `useDetailResource`

**Files:**
- Modify: `frontend/src/hooks/useDetailResource.ts`

The current implementation is 88 lines: manual `useState` × 4, `AbortController`, `useCallback` for fetch, `useEffect` for mount, `useResourceStream` with 500ms debounce for SSE, debounce cleanup.

- [ ] **Step 1: Rewrite the hook**

Replace `frontend/src/hooks/useDetailResource.ts`:

```typescript
import { api, emptyMethods, setsEqual } from "../api/client";
import type { FetchResult } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  ssePath: string,
) {
  const queryClient = useQueryClient();

  const resourceQuery = useQuery({
    queryKey: ["detail", ssePath],
    queryFn: async ({ signal }) => {
      const result = await fetchFn(key!, signal);
      return result;
    },
    enabled: !!key,
    retry: 1,
  });

  const historyQuery = useQuery({
    queryKey: ["history", { resourceId: key, limit: 10 }],
    queryFn: ({ signal }) => api.history({ resourceId: key!, limit: 10 }, signal),
    enabled: !!key,
    retry: false,
  });

  // SSE events trigger debounced invalidation.
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useResourceStream(
    ssePath,
    useCallback(() => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;
        queryClient.invalidateQueries({ queryKey: ["detail", ssePath] });
        queryClient.invalidateQueries({ queryKey: ["history", { resourceId: key }] });
      }, 500);
    }, [queryClient, ssePath, key]),
  );

  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);

  const data = resourceQuery.data?.data ?? null;
  const allowedMethods = resourceQuery.data?.allowedMethods ?? emptyMethods;
  const error = resourceQuery.error ?? null;
  const history = historyQuery.data ?? [];

  const retry = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ["detail", ssePath] });
    queryClient.invalidateQueries({ queryKey: ["history", { resourceId: key }] });
  }, [queryClient, ssePath, key]);

  return { data, history, error, retry, allowedMethods };
}
```

Key changes:
- `useState` × 4 → two `useQuery` calls
- Manual `AbortController` → TanStack Query handles abort via `signal`
- `fetchData` callback → `queryFn`
- SSE still uses 500ms debounce but calls `invalidateQueries` instead of `fetchData`
- `retry` callback calls `invalidateQueries` instead of re-fetching manually
- Return shape identical: `{ data, history, error, retry, allowedMethods }`
- Query key uses `ssePath` (which includes the resource ID, e.g., `/configs/abc123`) for uniqueness

The `FetchResult<T>` wrapper `{ data, allowedMethods }` is stored in the query cache. `data` and `allowedMethods` are projected from it in the return.

- [ ] **Step 2: Verify consumers compile without changes**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean — ConfigDetail, NetworkDetail, VolumeDetail, SecretDetail use the same `{ data, history, error, retry, allowedMethods }` return shape.

- [ ] **Step 3: Run tests**

Run: `npx vitest run`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add src/hooks/useDetailResource.ts
git commit -m "refactor(frontend): migrate useDetailResource to TanStack Query"
```

---

### Task 2: Verify Detail Pages Work Correctly

**Files:** None modified — this is a verification task.

- [ ] **Step 1: Check TypeScript compilation**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 2: Run all tests**

Run: `npx vitest run`
Expected: All pass.

- [ ] **Step 3: Lint and format**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint && make fmt-check`
Expected: Clean.

- [ ] **Step 4: Build**

Run: `make build`
Expected: Builds successfully.
