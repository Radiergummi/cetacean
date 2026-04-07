# TanStack Query Migration — Phase 1: Setup + Singleton Queries

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install TanStack Query, wire up the provider, and migrate all singleton (non-paginated, non-SSE) data fetching patterns — proving the approach before tackling the complex hooks in later phases.

**Architecture:** Install `@tanstack/react-query` and `@tanstack/react-query-devtools`. Add `QueryClientProvider` to the app root. Migrate 6 singleton patterns one at a time, each replacing manual `useState`/`useEffect`/`useCallback` fetch logic with `useQuery`. Each migration is independently committable.

**Tech Stack:** `@tanstack/react-query` v5, `@tanstack/react-query-devtools`, React 19, TypeScript, Vitest

---

## File Structure

- **Modify:** `frontend/package.json` — add dependencies
- **Create:** `frontend/src/lib/queryClient.ts` — QueryClient singleton with default config
- **Modify:** `frontend/src/App.tsx` — wrap app in `QueryClientProvider`
- **Modify:** `frontend/src/hooks/useMonitoringStatus.ts` — replace module-level cache with `useQuery`
- **Modify:** `frontend/src/hooks/useRecommendations.ts` — replace module-level cache with `useQuery`
- **Modify:** `frontend/src/pages/SearchPage.tsx` — replace manual fetch with `useQuery`
- **Modify:** `frontend/src/pages/PluginList.tsx` — replace manual fetch with `useQuery`
- **Modify:** `frontend/src/pages/ClusterOverview.tsx` — replace manual fetch with `useQuery` + SSE invalidation
- **Modify:** `frontend/src/components/DiskUsageSection.tsx` — replace manual fetch with `useQuery`
- **Modify:** test files as needed for QueryClientProvider wrapper

---

### Task 1: Install TanStack Query and Create QueryClient

**Files:**
- Modify: `frontend/package.json`
- Create: `frontend/src/lib/queryClient.ts`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: Install dependencies**

```bash
cd /Users/moritz/GolandProjects/cetacean/frontend
npm install @tanstack/react-query @tanstack/react-query-devtools
```

- [ ] **Step 2: Create QueryClient singleton**

Create `frontend/src/lib/queryClient.ts`:

```typescript
import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 0,
      gcTime: 5 * 60 * 1000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});
```

- [ ] **Step 3: Wrap app in QueryClientProvider**

In `frontend/src/App.tsx`, add imports:

```typescript
import { QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { queryClient } from "./lib/queryClient";
```

Wrap the outermost component inside `BrowserRouter` (alongside `AuthProvider`):

```tsx
export default function App() {
  return (
    <BrowserRouter basename={basePath}>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <ConnectionTracker>
            {/* ... existing children ... */}
          </ConnectionTracker>
        </AuthProvider>
        {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} />}
      </QueryClientProvider>
    </BrowserRouter>
  );
}
```

`QueryClientProvider` wraps `AuthProvider` so auth-dependent queries can invalidate on auth changes.

- [ ] **Step 4: Verify the app still works**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

Run: `npx vitest run`
Expected: All existing tests pass (no behavior change yet).

- [ ] **Step 5: Commit**

```bash
git add package.json package-lock.json src/lib/queryClient.ts src/App.tsx
git commit -m "feat(frontend): install TanStack Query and wire up QueryClientProvider"
```

---

### Task 2: Migrate `useMonitoringStatus`

**Files:**
- Modify: `frontend/src/hooks/useMonitoringStatus.ts`
- Modify: `frontend/src/pages/ClusterOverview.test.tsx` (remove `_resetMonitoringStatusCache`)

The current hook has 73 lines of module-level cache with TTL, in-flight deduplication, and mounted-ref tracking. TanStack Query handles all of this natively.

- [ ] **Step 1: Rewrite the hook**

Replace `frontend/src/hooks/useMonitoringStatus.ts`:

```typescript
import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";
import { useQuery } from "@tanstack/react-query";

export const monitoringStatusQueryKey = ["monitoring-status"] as const;

export function useMonitoringStatus(): MonitoringStatus | null {
  const { data } = useQuery({
    queryKey: monitoringStatusQueryKey,
    queryFn: () => api.monitoringStatus(),
    staleTime: 60_000,
    retry: false,
  });

  return data ?? null;
}

/**
 * Derives whether Prometheus is configured and reachable.
 */
export function isPrometheusReady(status: MonitoringStatus | null): boolean {
  return !!status?.prometheusConfigured && !!status?.prometheusReachable;
}

/**
 * Derives whether cAdvisor targets are available via Prometheus.
 */
export function isCadvisorReady(status: MonitoringStatus | null): boolean {
  return isPrometheusReady(status) && !!status?.cadvisor?.targets;
}

/**
 * Derives whether node-exporter targets are available via Prometheus.
 */
export function isNodeExporterReady(status: MonitoringStatus | null): boolean {
  return isPrometheusReady(status) && !!status?.nodeExporter?.targets;
}
```

This replaces 73 lines with 15 lines. The `staleTime: 60_000` matches the old 60s TTL. Request deduplication is automatic. The `_resetMonitoringStatusCache` export is no longer needed.

- [ ] **Step 2: Update tests that use `_resetMonitoringStatusCache`**

In `frontend/src/pages/ClusterOverview.test.tsx`, remove the import and call:

```typescript
// Remove: import { _resetMonitoringStatusCache } from "../hooks/useMonitoringStatus";
// Remove: _resetMonitoringStatusCache(); from beforeEach
```

The test already mocks `api.monitoringStatus`, which TanStack Query will call. Tests need a `QueryClientProvider` in their wrapper. Update the wrapper:

```typescript
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return (
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <>{children}</>
      </MemoryRouter>
    </QueryClientProvider>
  );
}
```

Creating a fresh `QueryClient` per test prevents cache leakage between tests.

- [ ] **Step 3: Check for other consumers of `_resetMonitoringStatusCache`**

Search for `_resetMonitoringStatusCache` across the codebase. Remove all references.

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run`
Expected: All pass.

Run: `npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 5: Commit**

```bash
git add src/hooks/useMonitoringStatus.ts src/pages/ClusterOverview.test.tsx
git commit -m "refactor(frontend): migrate useMonitoringStatus to TanStack Query"
```

---

### Task 3: Migrate `useRecommendations`

**Files:**
- Modify: `frontend/src/hooks/useRecommendations.ts`

The current hook has 105 lines of module-level cache with TTL, in-flight deduplication, subscriber pattern for invalidation, and route-change refetching. TanStack Query replaces all of it.

- [ ] **Step 1: Rewrite the hook**

Replace `frontend/src/hooks/useRecommendations.ts`:

```typescript
import { api } from "@/api/client";
import type { Recommendation, RecommendationSummary } from "@/api/types";
import { queryClient } from "@/lib/queryClient";
import { useQuery } from "@tanstack/react-query";

interface RecommendationsState {
  items: Recommendation[];
  summary: RecommendationSummary;
  total: number;
  hasData: boolean;
}

const emptyState: RecommendationsState = {
  items: [],
  summary: { critical: 0, warning: 0, info: 0 },
  total: 0,
  hasData: false,
};

export const recommendationsQueryKey = ["recommendations"] as const;

/**
 * Invalidates the recommendations cache and triggers a refetch for all consumers.
 * Call this after applying a recommendation fix.
 */
export function invalidateRecommendations() {
  queryClient.invalidateQueries({ queryKey: recommendationsQueryKey });
}

export function useRecommendations(): RecommendationsState {
  const { data } = useQuery({
    queryKey: recommendationsQueryKey,
    queryFn: async () => {
      const response = await api.recommendations();

      return {
        items: response.items ?? [],
        summary: response.summary,
        total: response.total,
        hasData: true,
      } satisfies RecommendationsState;
    },
    staleTime: 60_000,
    retry: false,
  });

  return data ?? emptyState;
}
```

This replaces 105 lines with 45 lines. The subscriber pattern, module-level cache, in-flight deduplication, and route-change refetching all disappear. `invalidateRecommendations()` is a one-liner calling `queryClient.invalidateQueries`.

The old hook refetched on route change via `useLocation().pathname` — TanStack Query's `staleTime: 60_000` achieves the same effect: navigating away and back within 60s serves cache, after 60s it refetches in the background.

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run`
Expected: All pass.

Run: `npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 3: Commit**

```bash
git add src/hooks/useRecommendations.ts
git commit -m "refactor(frontend): migrate useRecommendations to TanStack Query"
```

---

### Task 4: Migrate `SearchPage`

**Files:**
- Modify: `frontend/src/pages/SearchPage.tsx`

The current page has 30 lines of manual `useState`/`useEffect`/`AbortController` for the search query. Replace with `useQuery` keyed on the debounced query.

- [ ] **Step 1: Rewrite the fetch logic**

In `SearchPage.tsx`, replace the state and effect block (lines 36-71) with:

```typescript
import { useQuery } from "@tanstack/react-query";

export default function SearchPage() {
  const [input, query, setInput] = useSearchParam("q");

  const { data, isLoading: loading, error } = useQuery({
    queryKey: ["search", query],
    queryFn: ({ signal }) => api.search(query!, 0, signal),
    enabled: !!query,
    retry: false,
  });

  const errorMessage = error ? getErrorMessage(error, String(error)) : null;
```

Remove the `useState` for `data`, `loading`, `error`. Remove the `useEffect` with `AbortController`. TanStack Query handles abort automatically via the `signal` passed to `queryFn`.

The `enabled: !!query` option prevents the query from firing when query is empty (matching the current `if (!query) return` guard).

- [ ] **Step 2: Update the JSX**

The template references `data`, `loading`, `error` — update to match the new variable names. `data` is now `SearchResponse | undefined` instead of `SearchResponse | null`, so the `!query && !data` empty state check still works (`undefined` is falsy).

- [ ] **Step 3: Remove unused imports**

Remove `useEffect`, `useState` from the React import (keep `useCallback` etc. if still needed). Remove `AbortController` usage.

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add src/pages/SearchPage.tsx
git commit -m "refactor(frontend): migrate SearchPage to TanStack Query"
```

---

### Task 5: Migrate `PluginList`

**Files:**
- Modify: `frontend/src/pages/PluginList.tsx`

The current page has manual `useState`/`useEffect`/`useCallback` for fetching plugins. Replace with `useQuery`.

- [ ] **Step 1: Rewrite the fetch logic**

Replace the state and fetch block (lines 13-33) with:

```typescript
import { useQuery, useQueryClient } from "@tanstack/react-query";

export default function PluginList() {
  const [installOpen, setInstallOpen] = useState(false);
  const queryClient = useQueryClient();

  const {
    data: pluginData,
    error,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: ["plugins"],
    queryFn: () => api.plugins(),
  });

  const plugins = pluginData?.data ?? null;
  const allowedMethods = pluginData?.allowedMethods ?? emptyMethods;
```

The `FetchResult<Plugin[]>` return type from `api.plugins()` contains `{ data, allowedMethods }`. The query caches the full wrapper; the component destructures what it needs.

Replace the `fetchPlugins` retry callback with `refetch` from the query.

After plugin install/enable/disable/remove actions, call `queryClient.invalidateQueries({ queryKey: ["plugins"] })` to refetch.

- [ ] **Step 2: Update error and loading states**

Replace `if (error)` with `if (error)` (same pattern, `error` is now `Error | null` from useQuery).
Replace `if (!plugins)` loading check with `if (isLoading)`.

- [ ] **Step 3: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add src/pages/PluginList.tsx
git commit -m "refactor(frontend): migrate PluginList to TanStack Query"
```

---

### Task 6: Migrate `ClusterOverview`

**Files:**
- Modify: `frontend/src/pages/ClusterOverview.tsx`
- Modify: `frontend/src/pages/ClusterOverview.test.tsx`

The current page fetches `api.cluster()` and `api.history()` with manual state, then refetches on SSE events with a 2-second debounce. Replace with two `useQuery` calls + SSE-driven invalidation.

- [ ] **Step 1: Rewrite the fetch logic**

Replace the state and fetch block (lines 23-79) with:

```typescript
import { useQuery, useQueryClient } from "@tanstack/react-query";

export default function ClusterOverview() {
  const queryClient = useQueryClient();
  const prevRef = useRef<ClusterSnapshot | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const { data: snapshot } = useQuery({
    queryKey: ["cluster"],
    queryFn: () => api.cluster(),
  });

  const { data: history = [], isLoading: historyLoading } = useQuery({
    queryKey: ["history", { limit: 25 }],
    queryFn: () => api.history({ limit: 25 }),
  });

  // Track previous snapshot for trend indicators
  useEffect(() => {
    if (snapshot) {
      prevRef.current = snapshot;
    }
  }, [snapshot]);

  // SSE events trigger debounced invalidation
  useResourceStream(
    "/events",
    useCallback(() => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        queryClient.invalidateQueries({ queryKey: ["cluster"] });
        queryClient.invalidateQueries({ queryKey: ["history"] });
      }, 2_000);
    }, [queryClient]),
  );

  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);
```

Remove the `fetchSnapshot`, `fetchHistory` callbacks, and the `useEffect` that calls them on mount (TanStack Query handles initial fetch).

- [ ] **Step 2: Update the test**

In `ClusterOverview.test.tsx`, the test already mocks `api.cluster` and `api.history`. Add `QueryClientProvider` to the test wrapper (same pattern as Task 2). The mock `api.cluster` return now goes through `useQuery`, which calls it the same way.

- [ ] **Step 3: Remove unused imports**

Remove `useCallback` if no longer needed for anything else. Remove the `useState` for `snapshot`, `history`, `historyLoading`.

- [ ] **Step 4: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run src/pages/ClusterOverview.test.tsx`
Expected: Pass.

Run: `npx vitest run`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add src/pages/ClusterOverview.tsx src/pages/ClusterOverview.test.tsx
git commit -m "refactor(frontend): migrate ClusterOverview to TanStack Query"
```

---

### Task 7: Migrate `DiskUsageSection`

**Files:**
- Modify: `frontend/src/components/DiskUsageSection.tsx`

The current component has two `useEffect` blocks: one checks if the current node is the local node (to show/hide the section), the other fetches disk usage. Replace the disk usage fetch with `useQuery`.

- [ ] **Step 1: Rewrite the disk usage fetch**

Replace the second `useEffect` (lines 381-391) with `useQuery`:

```typescript
import { useQuery } from "@tanstack/react-query";

// Inside the component:
const { data, isLoading: loading } = useQuery({
  queryKey: ["disk-usage"],
  queryFn: () => api.diskUsage(),
  enabled: visible,
});
```

Keep the first `useEffect` that checks `localNodeID` — this determines `visible`, which gates the query via `enabled`.

Remove the `useState` for `data` and `loading`. The `loading` state is now `isLoading` from the query.

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add src/components/DiskUsageSection.tsx
git commit -m "refactor(frontend): migrate DiskUsageSection to TanStack Query"
```

---

### Task 8: Create Test Utility for QueryClientProvider

**Files:**
- Create: `frontend/src/test/queryWrapper.tsx`

Several tests will need a `QueryClientProvider` wrapper. Create a reusable utility.

- [ ] **Step 1: Create the utility**

Create `frontend/src/test/queryWrapper.tsx`:

```typescript
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";

/**
 * Creates a test wrapper with a fresh QueryClient and MemoryRouter.
 * Use a fresh client per test to prevent cache leakage.
 */
export function createTestWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
      },
    },
  });

  return function TestWrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <>{children}</>
        </MemoryRouter>
      </QueryClientProvider>
    );
  };
}
```

- [ ] **Step 2: Update `ClusterOverview.test.tsx` to use it**

Replace the hand-written wrapper with:

```typescript
import { createTestWrapper } from "../test/queryWrapper";

// In each test:
render(<ClusterOverview />, { wrapper: createTestWrapper() });
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx vitest run`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add src/test/queryWrapper.tsx src/pages/ClusterOverview.test.tsx
git commit -m "test(frontend): add reusable QueryClient test wrapper"
```

---

### Task 9: Full Verification

**Files:** None new.

- [ ] **Step 1: TypeScript check**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean.

- [ ] **Step 2: All frontend tests**

Run: `npx vitest run`
Expected: All pass.

- [ ] **Step 3: Lint**

Run: `cd /Users/moritz/GolandProjects/cetacean && make lint`
Expected: Clean.

- [ ] **Step 4: Format check**

Run: `make fmt-check`
Expected: Clean.

- [ ] **Step 5: Build**

Run: `make build`
Expected: Builds successfully. Check bundle size — TanStack Query adds ~15KB gzipped.
