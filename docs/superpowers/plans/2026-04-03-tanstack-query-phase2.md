# TanStack Query Migration — Phase 2: Metrics Hooks

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate all Prometheus metrics fetching hooks from manual `setInterval` + `useState`/`useEffect` to TanStack Query's `useQuery` with `refetchInterval`.

**Architecture:** The shared `useMetricsMap` hook is the core migration target — it replaces `setInterval` + cancellation with `useQuery` + `refetchInterval: 30_000`. The thin wrappers (`useNodeMetrics`, `useServiceMetrics`) stay as wrappers but become even simpler. Standalone patterns (`useInstanceResolver`, `useTaskMetrics`, `CapacitySection`, `NodeResourceGauges`) each get their own `useQuery` call.

**Tech Stack:** `@tanstack/react-query` v5 (already installed from Phase 1), React 19, TypeScript

---

## File Structure

- **Modify:** `frontend/src/hooks/useMetricsMap.ts` — replace setInterval with useQuery + refetchInterval
- **Modify:** `frontend/src/hooks/useInstanceResolver.ts` — replace setInterval with useQuery
- **Modify:** `frontend/src/hooks/useTaskMetrics.ts` — replace setInterval with useQuery
- **Modify:** `frontend/src/components/metrics/CapacitySection.tsx` — replace setInterval with useQuery
- **Modify:** `frontend/src/components/metrics/NodeResourceGauges.tsx` — replace setInterval with useQuery
- `useNodeMetrics` and `useServiceMetrics` may not need changes if `useMetricsMap` migration is transparent

---

### Task 1: Migrate `useMetricsMap`

**Files:**
- Modify: `frontend/src/hooks/useMetricsMap.ts`

This is the shared core used by `useNodeMetrics` and `useServiceMetrics`. The current implementation is 107 lines with `setInterval`, manual cancellation, and `Promise.all` for parallel queries.

- [ ] **Step 1: Rewrite the hook**

Replace `frontend/src/hooks/useMetricsMap.ts`:

```typescript
import { api } from "../api/client";
import { parseInstant, parseRange } from "../lib/prometheusParser";
import { useQuery } from "@tanstack/react-query";

interface InstantField<T> {
  query: string;
  assign: (metrics: T, value: number) => void;
}

interface RangeField<T> {
  query: string;
  assign: (metrics: T, values: number[]) => void;
}

export interface MetricsMapSpec<T> {
  labelKey: string;
  empty: () => T;
  instant?: readonly InstantField<T>[];
  range?: readonly RangeField<T>[];
}

/**
 * Fetches instant and range Prometheus queries, parses results keyed by a
 * label, and returns a Record<string, T> that auto-refreshes on an interval.
 */
export function useMetricsMap<T>(
  spec: MetricsMapSpec<T>,
  enabled: boolean,
  refreshInterval = 30_000,
): Record<string, T> {
  const { data = {} } = useQuery({
    queryKey: ["metrics-map", spec.labelKey, spec.instant, spec.range],
    queryFn: async () => {
      const now = Math.floor(Date.now() / 1000);
      const start = now - 3600;
      const step = 120;

      const [instantResponses, rangeResponses] = await Promise.all([
        Promise.all(
          (spec.instant ?? []).map(({ query }) =>
            api.metricsQuery(query).catch((error) => {
              console.warn(error);
              return null;
            }),
          ),
        ),
        Promise.all(
          (spec.range ?? []).map(({ query }) =>
            api.metricsQueryRange(query, String(start), String(now), String(step)).catch((error) => {
              console.warn(error);
              return null;
            }),
          ),
        ),
      ]);

      const map: Record<string, T> = {};

      const ensure = (key: string) => {
        if (!map[key]) {
          map[key] = spec.empty();
        }

        return map[key];
      };

      instantResponses.forEach((response, index) => {
        const field = spec.instant![index];
        parseInstant(response, spec.labelKey)?.forEach(([key, value]) => {
          field.assign(ensure(key), value);
        });
      });

      rangeResponses.forEach((response, index) => {
        const field = spec.range![index];
        parseRange(response, spec.labelKey)?.forEach(([key, values]) => {
          field.assign(ensure(key), values);
        });
      });

      return map;
    },
    enabled,
    refetchInterval: refreshInterval,
    retry: false,
    staleTime: refreshInterval,
  });

  return data as Record<string, T>;
}
```

Key changes:
- `setInterval` → `refetchInterval: 30_000`
- Manual `cancelled` flag → TanStack Query handles abort
- `useState` + `setByKey` → query cache
- `staleTime: refreshInterval` prevents refetch on remount within the interval
- Query key includes `spec.labelKey` and the query arrays for cache separation between node/service metrics

The return type stays `Record<string, T>` — `useNodeMetrics` and `useServiceMetrics` should work without changes since their interface to `useMetricsMap` is unchanged.

- [ ] **Step 2: Verify consumers still work**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Clean — `useNodeMetrics` and `useServiceMetrics` don't need changes.

- [ ] **Step 3: Run tests**

Run: `npx vitest run`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add src/hooks/useMetricsMap.ts
git commit -m "refactor(frontend): migrate useMetricsMap to TanStack Query"
```

---

### Task 2: Migrate `useInstanceResolver`

**Files:**
- Modify: `frontend/src/hooks/useInstanceResolver.ts`

The current implementation is 87 lines with a 60s `setInterval` and manual cancellation.

- [ ] **Step 1: Rewrite the hook**

Replace `frontend/src/hooks/useInstanceResolver.ts`:

```typescript
import { api } from "../api/client";
import { isPrometheusReady, useMonitoringStatus } from "./useMonitoringStatus";
import { useQuery } from "@tanstack/react-query";
import { useCallback } from "react";

export function useInstanceResolver() {
  const monitoring = useMonitoringStatus();
  const hasPrometheus = isPrometheusReady(monitoring);

  const { data: byHostname = {} } = useQuery({
    queryKey: ["instance-resolver"],
    queryFn: async () => {
      const response = await api.metricsQuery("node_uname_info");
      const map: Record<string, string> = {};

      for (const result of response.data.result) {
        const nodename = result.metric.nodename;
        const instance = result.metric.instance;

        if (nodename && instance) {
          map[nodename] = instance;
        }
      }

      return map;
    },
    enabled: hasPrometheus,
    refetchInterval: 60_000,
    staleTime: 60_000,
    retry: false,
  });

  const resolve = useCallback(
    (hostname: string): string | null => {
      if (byHostname[hostname]) {
        return byHostname[hostname];
      }

      const short = hostname.split(".")[0];

      if (short && byHostname[short]) {
        return byHostname[short];
      }

      for (const [name, instance] of Object.entries(byHostname)) {
        if (hostname.startsWith(name)) {
          return instance;
        }
      }

      return null;
    },
    [byHostname],
  );

  return {
    resolve,
    hasData: Object.keys(byHostname).length > 0,
  };
}
```

The `resolve` callback and return shape are identical — consumers don't need changes.

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add src/hooks/useInstanceResolver.ts
git commit -m "refactor(frontend): migrate useInstanceResolver to TanStack Query"
```

---

### Task 3: Migrate `useTaskMetrics`

**Files:**
- Modify: `frontend/src/hooks/useTaskMetrics.ts`

The current implementation is 109 lines with a `setInterval`, filter-based range queries, and manual cancellation.

- [ ] **Step 1: Rewrite the hook**

Replace `frontend/src/hooks/useTaskMetrics.ts`:

```typescript
import { api } from "../api/client";
import { useQuery } from "@tanstack/react-query";

export interface TaskMetricsData {
  cpu: number[];
  memory: number[];
  currentCpu: number | null;
  currentMemory: number | null;
}

const emptyMap = new Map<string, TaskMetricsData>();
const taskIdLabel = "container_label_com_docker_swarm_task_id";

export function useTaskMetrics(
  filter: string,
  enabled: boolean,
  refreshInterval = 30_000,
): Map<string, TaskMetricsData> {
  const { data = emptyMap } = useQuery({
    queryKey: ["task-metrics", filter],
    queryFn: async () => {
      const now = Math.floor(Date.now() / 1000);
      const start = String(now - 3600);
      const end = String(now);
      const step = "60";

      const cpuQuery = `sum by (${taskIdLabel})(rate(container_cpu_usage_seconds_total{${filter}}[5m])) * 100`;
      const memQuery = `sum by (${taskIdLabel})(container_memory_usage_bytes{${filter}})`;

      const [cpuResponse, memoryResponse] = await Promise.all([
        api.metricsQueryRange(cpuQuery, start, end, step),
        api.metricsQueryRange(memQuery, start, end, step),
      ]);

      const map = new Map<string, TaskMetricsData>();

      for (const series of cpuResponse.data?.result ?? []) {
        const taskId = series.metric[taskIdLabel];

        if (!taskId || !series.values?.length) {
          continue;
        }

        const values = series.values.map(([, value]: [number, string]) => parseFloat(value));

        map.set(taskId, {
          cpu: values,
          memory: [],
          currentCpu: values[values.length - 1] ?? null,
          currentMemory: null,
        });
      }

      for (const series of memoryResponse.data?.result ?? []) {
        const taskId = series.metric[taskIdLabel];

        if (!taskId || !series.values?.length) {
          continue;
        }

        const values = series.values.map(([, value]: [number, string]) => parseFloat(value));
        const existing = map.get(taskId);

        if (existing) {
          existing.memory = values;
          existing.currentMemory = values[values.length - 1] ?? null;
        } else {
          map.set(taskId, {
            cpu: [],
            memory: values,
            currentCpu: null,
            currentMemory: values[values.length - 1] ?? null,
          });
        }
      }

      return map;
    },
    enabled: enabled && !!filter,
    refetchInterval: refreshInterval,
    staleTime: refreshInterval,
    retry: false,
  });

  return data;
}
```

The return type stays `Map<string, TaskMetricsData>` — consumers don't need changes. The `enabled: enabled && !!filter` check replaces the early `setMetrics(emptyMap); return` guard.

- [ ] **Step 2: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add src/hooks/useTaskMetrics.ts
git commit -m "refactor(frontend): migrate useTaskMetrics to TanStack Query"
```

---

### Task 4: Migrate `CapacitySection` and `NodeResourceGauges`

**Files:**
- Modify: `frontend/src/components/metrics/CapacitySection.tsx`
- Modify: `frontend/src/components/metrics/NodeResourceGauges.tsx`

Both use the same pattern: `useState` + `useEffect` + `setInterval(load, 30_000)`.

- [ ] **Step 1: Rewrite `CapacitySection` fetch**

In `CapacitySection.tsx`, replace the `useState` + `useEffect` + `setInterval` block (lines 66-92) with:

```typescript
import { useQuery } from "@tanstack/react-query";

// Inside the component:
const { data: metrics } = useQuery({
  queryKey: ["cluster-metrics"],
  queryFn: () => api.clusterMetrics(),
  enabled: prometheusConfigured,
  refetchInterval: 30_000,
  staleTime: 30_000,
  retry: false,
});
```

Remove `useState` for metrics, `useEffect`, `setInterval`, the `cancelled` flag.

- [ ] **Step 2: Rewrite `NodeResourceGauges` fetch**

In `NodeResourceGauges.tsx`, replace the `useState` + `useCallback` + `useEffect` + `setInterval` block (lines 53-78) with:

```typescript
import { useQuery } from "@tanstack/react-query";

// Inside the component:
const { data: values = gauges.map(() => null) } = useQuery({
  queryKey: ["node-resource-gauges", instance],
  queryFn: () =>
    Promise.all(
      gauges.map((gauge) =>
        api
          .metricsQuery(gauge.query(instance))
          .then((response) => {
            const value = response.data?.result?.[0]?.value?.[1];
            return value != null ? Number(value) : null;
          })
          .catch(() => null),
      ),
    ),
  refetchInterval: 30_000,
  staleTime: 30_000,
  retry: false,
});
```

Remove `useState` for values, `useCallback` for fetchAll, `useEffect`, `setInterval`.

- [ ] **Step 3: Run tests**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit && npx vitest run`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add src/components/metrics/CapacitySection.tsx src/components/metrics/NodeResourceGauges.tsx
git commit -m "refactor(frontend): migrate CapacitySection and NodeResourceGauges to TanStack Query"
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
Expected: Clean.

- [ ] **Step 4: Build**

Run: `make build`
Expected: Builds successfully.
