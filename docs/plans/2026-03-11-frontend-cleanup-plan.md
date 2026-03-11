# Frontend Cleanup Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix bugs and inconsistencies found during frontend code review — incorrect hook behavior, redundant code, double renders, and minor quality issues.

**Architecture:** Each task is an independent fix touching 1-3 files. No new features — only corrections to existing code. Tasks are ordered by impact but can be done in any order.

**Tech Stack:** React 19, TypeScript, Vitest

---

## File Map

| File | Change |
|------|--------|
| `frontend/src/hooks/useDebouncedCallback.ts` | Delete (unused) |
| `frontend/src/hooks/useSwarmResource.ts` | Remove redundant ref assignment in effect |
| `frontend/src/hooks/useSwarmResource.test.tsx` | No change (existing tests cover the fix) |
| `frontend/src/hooks/useSort.ts` | Remove double-render: derive sort state from URL directly |
| `frontend/src/hooks/useMonitoringStatus.ts` | Don't cache errors permanently |
| `frontend/src/pages/ClusterOverview.tsx` | Fix `historyLoading` race |
| `frontend/src/pages/StackDetail.tsx` | Debounce SSE-driven refetch to prevent N-call burst |
| `frontend/src/pages/SearchPage.tsx` | Merge split imports |
| `frontend/src/components/log/useLogFilter.ts` | Remove redundant stream filter |
| `frontend/src/components/search/SearchPalette.tsx` | Fix `!!response` in effect deps |
| `frontend/src/components/log/LogToolbar.tsx` | Add `type="button"` to ToolbarButton |

---

## Chunk 1: Hook Fixes

### Task 1: Delete unused `useDebouncedCallback`

**Files:**
- Delete: `frontend/src/hooks/useDebouncedCallback.ts`

The hook is unused (zero imports across the codebase) and implements throttle behavior mislabeled as debounce. Delete it.

- [ ] **Step 1: Verify no imports**

Run: `cd /Users/moritz/GolandProjects/cetacean && grep -r "useDebouncedCallback" frontend/src --include="*.ts" --include="*.tsx" -l`
Expected: only `frontend/src/hooks/useDebouncedCallback.ts` itself

- [ ] **Step 2: Delete the file**

```bash
rm frontend/src/hooks/useDebouncedCallback.ts
```

- [ ] **Step 3: Verify build**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: delete unused useDebouncedCallback hook (mislabeled throttle)"
```

---

### Task 2: Fix `useSwarmResource` redundant ref assignment

**Files:**
- Modify: `frontend/src/hooks/useSwarmResource.ts`
- Test: `frontend/src/hooks/useSwarmResource.test.tsx` (existing tests, no changes)

The `useEffect` on lines 32-35 redundantly assigns `fetchFnRef.current = fetchFn` (already done on line 17) and triggers `load()` on every `fetchFn` reference change. Since callers already stabilize `fetchFn` with `useCallback`, the effect works but the redundant assignment is confusing. Remove it.

- [ ] **Step 1: Remove redundant assignment**

In `frontend/src/hooks/useSwarmResource.ts`, replace:

```typescript
  useEffect(() => {
    fetchFnRef.current = fetchFn;
    load();
  }, [fetchFn]);
```

With:

```typescript
  useEffect(() => {
    load();
  }, [fetchFn]);
```

- [ ] **Step 2: Run existing tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/hooks/useSwarmResource.test.tsx`
Expected: all 5 tests pass

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useSwarmResource.ts
git commit -m "fix: remove redundant fetchFnRef assignment in useSwarmResource"
```

---

### Task 3: Fix `useSort` double-render

**Files:**
- Modify: `frontend/src/hooks/useSort.ts`

Both `useSort` and `useSortParams` maintain local state that duplicates the URL params, causing a double render on every toggle: once from `setSortKey`/`setSortDir`, once from the `useEffect` re-syncing from `params`. Fix by deriving state directly from URL params instead of syncing.

- [ ] **Step 1: Rewrite `useSort` to derive from URL**

Replace the entire `useSort` function (lines 8-77) with:

```typescript
export function useSort<T>(
  items: T[],
  accessors: Record<string, Accessor<T>>,
  defaultKey?: string,
  defaultDir: SortDir = "asc",
) {
  const [params, setParams] = useSearchParams();
  const sortKey = params.get("sort") ?? defaultKey;
  const sortDir = (
    params.get("dir") === "desc" ? "desc" : params.get("dir") === "asc" ? "asc" : defaultDir
  ) as SortDir;

  const toggle = useCallback(
    (key: string) => {
      const newDir = sortKey === key ? (sortDir === "asc" ? "desc" : "asc") : "asc";
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (key === defaultKey && newDir === defaultDir) {
            next.delete("sort");
            next.delete("dir");
          } else {
            next.set("sort", key);
            next.set("dir", newDir);
          }
          return next;
        },
        { replace: true },
      );
    },
    [sortKey, sortDir, defaultKey, defaultDir, setParams],
  );

  const sorted = useMemo(() => {
    if (!sortKey || !accessors[sortKey]) return items;
    const get = accessors[sortKey];
    return [...items].sort((a, b) => {
      const av = get(a);
      const bv = get(b);
      let cmp: number;
      if (typeof av === "number" && typeof bv === "number") {
        cmp = av - bv;
      } else {
        cmp = String(av ?? "").localeCompare(String(bv ?? ""));
      }
      return sortDir === "asc" ? cmp : -cmp;
    });
  }, [items, sortKey, sortDir, accessors]);

  return { sorted, sortKey, sortDir, toggle };
}
```

- [ ] **Step 2: Rewrite `useSortParams` to derive from URL**

Replace the entire `useSortParams` function (lines 79-119) with:

```typescript
/** Sort state only — no client-side sorting. For use with server-side sort. */
export function useSortParams(defaultKey?: string, defaultDir: SortDir = "asc") {
  const [params, setParams] = useSearchParams();
  const sortKey = params.get("sort") ?? defaultKey;
  const sortDir = (
    params.get("dir") === "desc" ? "desc" : params.get("dir") === "asc" ? "asc" : defaultDir
  ) as SortDir;

  const toggle = useCallback(
    (key: string) => {
      const newDir = sortKey === key ? (sortDir === "asc" ? "desc" : "asc") : "asc";
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (key === defaultKey && newDir === defaultDir) {
            next.delete("sort");
            next.delete("dir");
          } else {
            next.set("sort", key);
            next.set("dir", newDir);
          }
          return next;
        },
        { replace: true },
      );
    },
    [sortKey, sortDir, defaultKey, defaultDir, setParams],
  );

  return { sortKey, sortDir, toggle };
}
```

- [ ] **Step 3: Add `useCallback` import**

Ensure the import line at top of file reads:

```typescript
import { useCallback, useMemo } from "react";
```

(Remove `useState` and `useEffect` from imports since they are no longer used.)

- [ ] **Step 4: Run type check and tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: no errors, all tests pass

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/useSort.ts
git commit -m "fix: derive sort state from URL params to eliminate double render"
```

---

### Task 4: Fix `useMonitoringStatus` permanent error caching

**Files:**
- Modify: `frontend/src/hooks/useMonitoringStatus.ts`

On transient errors, the hook caches a synthetic "unconfigured" status permanently. Fix: on error, don't cache — allow retry on next mount.

- [ ] **Step 1: Fix error handling**

In `frontend/src/hooks/useMonitoringStatus.ts`, replace:

```typescript
        .catch(() => {
          cached = {
            prometheusConfigured: false,
            prometheusReachable: false,
            nodeExporter: null,
            cadvisor: null,
          };
        });
```

With:

```typescript
        .catch(() => {
          inflight = null;
        });
```

This means on error, `cached` stays `null` and `inflight` is cleared, so the next component mount retries the fetch. The hook returns `null` (loading state) until a successful response.

- [ ] **Step 2: Run type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useMonitoringStatus.ts
git commit -m "fix: don't permanently cache monitoring status on transient errors"
```

---

## Chunk 2: Page and Component Fixes

### Task 5: Fix `ClusterOverview` historyLoading race

**Files:**
- Modify: `frontend/src/pages/ClusterOverview.tsx`

`setHistoryLoading(false)` fires synchronously before the fetch resolves. Fix by removing the loading state — `history` starting as `[]` with `historyLoading` starting as `false` means ActivityFeed just renders empty initially, which is fine since the fetch is fast. Alternatively, move it into the `.then()`. Simplest fix: remove `historyLoading` entirely since `history.length === 0` before the first fetch resolves is visually acceptable (ActivityFeed already handles empty arrays).

- [ ] **Step 1: Remove historyLoading state**

Remove `const [historyLoading, setHistoryLoading] = useState(true);` (line 16).

Remove `setHistoryLoading(false);` from the useEffect (line 41).

Change the ActivityFeed prop from `loading={historyLoading}` to `loading={false}`.

Actually — looking at this more carefully, the simplest correct fix is to just move `setHistoryLoading(false)` into the fetch callback:

Replace:

```typescript
  useEffect(() => {
    fetchSnapshot();
    fetchHistory();
    setHistoryLoading(false);
  }, [fetchSnapshot, fetchHistory]);
```

With:

```typescript
  useEffect(() => {
    fetchSnapshot();
    fetchHistory();
  }, [fetchSnapshot, fetchHistory]);
```

And update `fetchHistory`:

Replace:

```typescript
  const fetchHistory = useCallback(() => {
    api
      .history({ limit: 25 })
      .then(setHistory)
      .catch(() => {});
  }, []);
```

With:

```typescript
  const fetchHistory = useCallback(() => {
    api
      .history({ limit: 25 })
      .then((h) => {
        setHistory(h);
        setHistoryLoading(false);
      })
      .catch(() => {
        setHistoryLoading(false);
      });
  }, []);
```

- [ ] **Step 2: Run type check and tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run src/pages/ClusterOverview.test.tsx`
Expected: all pass

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/ClusterOverview.tsx
git commit -m "fix: set historyLoading=false after fetch resolves, not synchronously"
```

---

### Task 6: Debounce StackDetail SSE refetch

**Files:**
- Modify: `frontend/src/pages/StackDetail.tsx`

Every task SSE event triggers `fetchData()`, which re-fetches the stack (new object ref), which triggers the task-counts effect (N parallel `api.serviceTasks()` calls). Fix by debouncing the SSE handler.

- [ ] **Step 1: Add debounce to SSE handler**

In `frontend/src/pages/StackDetail.tsx`, add a ref-based debounce. Replace:

```typescript
    useSSE(["stack", "service", "task"], useCallback(() => {
        fetchData();
    }, [fetchData]));
```

With:

```typescript
    const fetchTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
    useSSE(["stack", "service", "task"], useCallback(() => {
        clearTimeout(fetchTimerRef.current);
        fetchTimerRef.current = setTimeout(fetchData, 500);
    }, [fetchData]));
```

Add `useRef` to the imports at top of file (line 2):

```typescript
import {useCallback, useEffect, useRef, useState} from "react";
```

- [ ] **Step 2: Run type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/StackDetail.tsx
git commit -m "fix: debounce StackDetail SSE handler to prevent N-call API burst"
```

---

### Task 7: Merge split imports in SearchPage

**Files:**
- Modify: `frontend/src/pages/SearchPage.tsx`

Two imports from `searchConstants` with a component definition between them.

- [ ] **Step 1: Merge imports**

In `frontend/src/pages/SearchPage.tsx`, replace:

```typescript
import { statusColor } from "../lib/searchConstants";

function StateOrb({ state }: { state: string }) {
  if (state === "updating") {
    return <Loader2 className="size-3 shrink-0 text-blue-500 animate-spin" />;
  }
  const color = statusColor(state);
  return <span className={`inline-block size-2 rounded-full shrink-0 ${color}`} title={state} />;
}
import { resourcePath, TYPE_LABELS, TYPE_ORDER } from "../lib/searchConstants";
```

With:

```typescript
import { resourcePath, statusColor, TYPE_LABELS, TYPE_ORDER } from "../lib/searchConstants";

function StateOrb({ state }: { state: string }) {
  if (state === "updating") {
    return <Loader2 className="size-3 shrink-0 text-blue-500 animate-spin" />;
  }
  const color = statusColor(state);
  return <span className={`inline-block size-2 rounded-full shrink-0 ${color}`} title={state} />;
}
```

- [ ] **Step 2: Run lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/SearchPage.tsx
git commit -m "fix: merge split imports from searchConstants in SearchPage"
```

---

### Task 8: Remove redundant stream filter in useLogFilter

**Files:**
- Modify: `frontend/src/components/log/useLogFilter.ts`

Stream filtering is already applied server-side by `useLogData`. The client-side re-filter in `useLogFilter` is dead code.

- [ ] **Step 1: Remove stream filter and parameter**

In `frontend/src/components/log/useLogFilter.ts`, change the function signature:

```typescript
export function useLogFilter(lines: LogLine[]) {
```

Remove lines 15-17:

```typescript
    if (streamFilter !== "all") {
      result = result.filter(({ stream }) => stream === streamFilter);
    }
```

Remove `streamFilter` from the `useMemo` dependency array (line 44):

```typescript
  }, [lines, search, caseSensitive, useRegex, levelFilter, taskFilter]);
```

- [ ] **Step 2: Update caller in LogViewer**

In `frontend/src/components/log/LogViewer.tsx`, change the call to `useLogFilter` (around line 73):

From:
```typescript
  } = useLogFilter(data.lines, streamFilter);
```
To:
```typescript
  } = useLogFilter(data.lines);
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/components/log/LogViewer.test.tsx`
Expected: all 16 tests pass

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/log/useLogFilter.ts frontend/src/components/log/LogViewer.tsx
git commit -m "fix: remove redundant client-side stream filter (already applied server-side)"
```

---

### Task 9: Fix SearchPalette effect dependency

**Files:**
- Modify: `frontend/src/components/search/SearchPalette.tsx`

`!!response` in dependency array bypasses linter. The effect only needs to restart the polling interval when `query` changes; the `response` gate is already handled by the early return inside the effect body.

- [ ] **Step 1: Simplify dependency**

In `frontend/src/components/search/SearchPalette.tsx`, change line 147 from:

```typescript
  }, [query, !!response]);
```

To:

```typescript
  }, [query, response]);
```

Using `response` directly is correct: when it changes from null to an object (or vice versa), the effect restarts. The `if (!response || !query.trim()) return;` guard at the top of the effect body handles the null case.

- [ ] **Step 2: Run type check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/search/SearchPalette.tsx
git commit -m "fix: use stable dependency in SearchPalette polling effect"
```

---

### Task 10: Add `type="button"` to LogToolbar ToolbarButton

**Files:**
- Modify: `frontend/src/components/log/LogToolbar.tsx`

`ToolbarButton` renders `<button>` without `type="button"`, inconsistent with `LogTable.tsx` which includes it.

- [ ] **Step 1: Add type attribute**

In `frontend/src/components/log/LogToolbar.tsx`, in the `ToolbarButton` component (around line 206), change:

```tsx
    <button
      onClick={onClick}
```

To:

```tsx
    <button
      type="button"
      onClick={onClick}
```

- [ ] **Step 2: Run lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/log/LogToolbar.tsx
git commit -m "fix: add type=button to ToolbarButton for consistency"
```

---

### Task 11: Final verification

- [ ] **Step 1: Run full frontend checks**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run && npm run lint`
Expected: all pass
