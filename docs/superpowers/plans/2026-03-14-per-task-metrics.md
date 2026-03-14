# Per-Task Resource Usage Metrics Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show per-task CPU and memory usage as sparklines in all task tables and as gauges + charts on the task detail page.

**Architecture:** A single `useTaskMetrics` hook batch-fetches per-task CPU and memory time series from Prometheus (via cAdvisor labels) with one query per metric per table. A `TaskSparkline` wrapper around the existing `Sparkline` component renders inline in table cells. Task detail uses existing `ResourceGauge` and `MetricsPanel` components.

**Tech Stack:** React 19, TypeScript, Prometheus query_range API, existing Sparkline/ResourceGauge/MetricsPanel components, cAdvisor container labels.

**Spec:** `docs/superpowers/specs/2026-03-14-per-task-metrics-design.md`

---

### Task 1: Create `useTaskMetrics` Hook

**Files:**
- Create: `frontend/src/hooks/useTaskMetrics.ts`

- [ ] **Step 1: Create the hook file**

```typescript
// frontend/src/hooks/useTaskMetrics.ts
import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api/client";

export interface TaskMetricsData {
  cpu: number[];       // values only (for sparkline)
  memory: number[];    // values only (for sparkline)
  currentCpu: number | null;
  currentMemory: number | null;
}

const EMPTY_MAP = new Map<string, TaskMetricsData>();
const TASK_ID_LABEL = "container_label_com_docker_swarm_task_id";

export function useTaskMetrics(
  filter: string,
  enabled: boolean,
  refreshInterval = 30_000,
): Map<string, TaskMetricsData> {
  const [metrics, setMetrics] = useState<Map<string, TaskMetricsData>>(EMPTY_MAP);
  const intervalRef = useRef<ReturnType<typeof setInterval>>();

  useEffect(() => {
    if (!enabled) {
      setMetrics(EMPTY_MAP);
      return;
    }

    const fetchMetrics = async () => {
      const now = Math.floor(Date.now() / 1000);
      const start = String(now - 3600); // 1h
      const end = String(now);
      const step = "60";

      const cpuQuery = `sum by (${TASK_ID_LABEL})(rate(container_cpu_usage_seconds_total{${filter}}[5m])) * 100`;
      const memQuery = `sum by (${TASK_ID_LABEL})(container_memory_usage_bytes{${filter}})`;

      try {
        const [cpuResp, memResp] = await Promise.all([
          api.metricsQueryRange(cpuQuery, start, end, step),
          api.metricsQueryRange(memQuery, start, end, step),
        ]);

        const map = new Map<string, TaskMetricsData>();

        for (const series of cpuResp.data?.result ?? []) {
          const taskId = series.metric[TASK_ID_LABEL];
          if (!taskId || !series.values?.length) continue;
          const values = series.values.map((v) => parseFloat(v[1]));
          map.set(taskId, {
            cpu: values,
            memory: [],
            currentCpu: values[values.length - 1] ?? null,
            currentMemory: null,
          });
        }

        for (const series of memResp.data?.result ?? []) {
          const taskId = series.metric[TASK_ID_LABEL];
          if (!taskId || !series.values?.length) continue;
          const values = series.values.map((v) => parseFloat(v[1]));
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

        setMetrics(map);
      } catch {
        // Silently fail — metrics are non-critical
      }
    };

    fetchMetrics();
    intervalRef.current = setInterval(fetchMetrics, refreshInterval);
    return () => clearInterval(intervalRef.current);
  }, [filter, enabled, refreshInterval]);

  return metrics;
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```
git add frontend/src/hooks/useTaskMetrics.ts
git commit -m "feat: add useTaskMetrics hook for batch per-task metrics"
```

---

### Task 2: Create `TaskSparkline` Component

**Files:**
- Create: `frontend/src/components/metrics/TaskSparkline.tsx`
- Modify: `frontend/src/components/metrics/index.ts`

- [ ] **Step 1: Create the component**

```tsx
// frontend/src/components/metrics/TaskSparkline.tsx
import { formatBytes } from "../../lib/formatBytes";
import Sparkline from "./Sparkline";

interface Props {
  data: number[] | undefined;
  currentValue: number | null | undefined;
  type: "cpu" | "memory";
}

const COLORS = {
  cpu: "#4f8cf6",
  memory: "#34d399",
};

function formatValue(value: number | null | undefined, type: "cpu" | "memory"): string {
  if (value == null) return "\u2014";
  if (type === "cpu") return `${value.toFixed(1)}%`;
  return formatBytes(value);
}

export default function TaskSparkline({ data, currentValue, type }: Props) {
  if (!data?.length) {
    return <span className="text-muted-foreground text-xs">{"\u2014"}</span>;
  }

  return (
    <span className="inline-flex items-center gap-1.5">
      <Sparkline data={data} color={COLORS[type]} />
      <span className="text-xs tabular-nums whitespace-nowrap">
        {formatValue(currentValue, type)}
      </span>
    </span>
  );
}
```

- [ ] **Step 2: Export from the metrics index**

Add to `frontend/src/components/metrics/index.ts`:

```typescript
export { default as TaskSparkline } from "./TaskSparkline";
```

- [ ] **Step 3: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```
git add frontend/src/components/metrics/TaskSparkline.tsx frontend/src/components/metrics/index.ts
git commit -m "feat: add TaskSparkline component wrapping existing Sparkline"
```

---

### Task 3: Add Sparkline Columns to `TasksTable`

**Files:**
- Modify: `frontend/src/components/TasksTable.tsx`

The `TasksTable` component is used on both service detail and node detail pages. Add optional `metrics` prop and two new columns (CPU, Memory) after the State column.

- [ ] **Step 1: Update the interface and add imports**

In `frontend/src/components/TasksTable.tsx`, add the import and update the props interface.

Add import at line 4 (after `types` import):
```typescript
import type { TaskMetricsData } from "../hooks/useTaskMetrics";
import { TaskSparkline } from "./metrics";
```

Update the interface (line 13-16) to:
```typescript
interface TasksTableProps {
  tasks: Task[];
  variant: Variant;
  metrics?: Map<string, TaskMetricsData>;
}
```

Update the destructured props (line 18) to:
```typescript
export default function TasksTable({ tasks, variant, metrics }: TasksTableProps) {
```

- [ ] **Step 2: Add CPU and Memory header columns**

In the `<thead>` row (after the State `<th>` at line 55), add:

```tsx
{metrics && <th className="text-left p-3 text-sm font-medium">CPU</th>}
{metrics && <th className="text-left p-3 text-sm font-medium">Memory</th>}
```

- [ ] **Step 3: Add CPU and Memory data cells**

In the `<tbody>` row, after the State `<td>` (after line 98), add:

```tsx
{metrics && (
  <td className="p-3 text-sm">
    {State === "running" ? (
      <TaskSparkline
        data={metrics.get(ID)?.cpu}
        currentValue={metrics.get(ID)?.currentCpu}
        type="cpu"
      />
    ) : "\u2014"}
  </td>
)}
{metrics && (
  <td className="p-3 text-sm">
    {State === "running" ? (
      <TaskSparkline
        data={metrics.get(ID)?.memory}
        currentValue={metrics.get(ID)?.currentMemory}
        type="memory"
      />
    ) : "\u2014"}
  </td>
)}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 5: Commit**

```
git add frontend/src/components/TasksTable.tsx
git commit -m "feat: add CPU and Memory sparkline columns to TasksTable"
```

---

### Task 4: Wire Up Metrics on Service Detail Page

**Files:**
- Modify: `frontend/src/pages/ServiceDetail.tsx`

- [ ] **Step 1: Add import and hook call**

Add import (after line 10, with the other imports):
```typescript
import { useTaskMetrics } from "../hooks/useTaskMetrics";
```

Inside `ServiceDetail()`, after `const hasPrometheus = ...` (line 32), add:

```typescript
const hasCadvisor = !!monitoring?.cadvisor?.activeTargets;
const serviceName = service?.Spec.Name || "";
const runningTaskIds = useMemo(
  () => tasks.filter((t) => t.Status.State === "running").map((t) => t.ID),
  [tasks],
);
const taskMetrics = useTaskMetrics(
  serviceName
    ? `container_label_com_docker_swarm_service_name="${escapePromQL(serviceName)}"`
    : "",
  hasCadvisor && !!serviceName,
);
```

Note: `useMemo` must be added to the React import on line 1: change `{ useCallback, useEffect, useState }` to `{ useCallback, useEffect, useMemo, useState }`. `escapePromQL` is already imported on line 10.

- [ ] **Step 2: Pass metrics to TasksTable**

Update the `<TasksTable>` call (line 124) from:
```tsx
<TasksTable tasks={tasks} variant="service" />
```
to:
```tsx
<TasksTable tasks={tasks} variant="service" metrics={hasCadvisor ? taskMetrics : undefined} />
```

- [ ] **Step 3: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```
git add frontend/src/pages/ServiceDetail.tsx
git commit -m "feat: show per-task CPU/memory sparklines on service detail"
```

---

### Task 5: Wire Up Metrics on Node Detail Page

**Files:**
- Modify: `frontend/src/pages/NodeDetail.tsx`

- [ ] **Step 1: Add import and hook call**

Add import (after line 1):
```typescript
import { useTaskMetrics } from "../hooks/useTaskMetrics";
```

Inside `NodeDetail()`, after the `hasPrometheus` line (line 28), add:

```typescript
const hasCadvisor = !!monitoring?.cadvisor?.activeTargets;
```

After `const [error, setError] = ...` (line 30) and after the node is loaded (we need `node` to exist for the ID), add the hook call before the return statements but after the early returns. Since the hook must be called unconditionally (React rules), place it after `const [error, setError]`:

```typescript
const nodeId = node?.ID || "";
const taskMetrics = useTaskMetrics(
  nodeId ? `container_label_com_docker_swarm_node_id="${escapePromQL(nodeId)}"` : "",
  hasCadvisor && !!nodeId,
);
```

`escapePromQL` is already imported on line 2.

- [ ] **Step 2: Pass metrics to TasksTable**

Update the `<TasksTable>` call (line 115) from:
```tsx
<TasksTable tasks={tasks} variant="node" />
```
to:
```tsx
<TasksTable tasks={tasks} variant="node" metrics={hasCadvisor ? taskMetrics : undefined} />
```

- [ ] **Step 3: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```
git add frontend/src/pages/NodeDetail.tsx
git commit -m "feat: show per-task CPU/memory sparklines on node detail"
```

---

### Task 6: Add Sparklines to Task List Page

**Files:**
- Modify: `frontend/src/pages/TaskList.tsx`

- [ ] **Step 1: Add imports**

Add imports after line 1:
```typescript
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { TaskSparkline } from "../components/metrics";
```

- [ ] **Step 2: Add hook calls**

Inside `TaskList()`, after the `useSwarmResource` call (after line 34), add:

```typescript
const monitoring = useMonitoringStatus();
const hasCadvisor = !!monitoring?.cadvisor?.activeTargets;
const taskMetrics = useTaskMetrics(
  `container_label_com_docker_swarm_task_id!=""`,
  hasCadvisor,
);
```

- [ ] **Step 3: Add CPU and Memory columns**

In the `columns` array, after the "State" column object (after line 52), add:

```typescript
...(hasCadvisor
  ? [
      {
        header: "CPU",
        cell: (t: Task) =>
          t.Status.State === "running" ? (
            <TaskSparkline
              data={taskMetrics.get(t.ID)?.cpu}
              currentValue={taskMetrics.get(t.ID)?.currentCpu}
              type="cpu"
            />
          ) : (
            "\u2014"
          ),
      },
      {
        header: "Memory",
        cell: (t: Task) =>
          t.Status.State === "running" ? (
            <TaskSparkline
              data={taskMetrics.get(t.ID)?.memory}
              currentValue={taskMetrics.get(t.ID)?.currentMemory}
              type="memory"
            />
          ) : (
            "\u2014"
          ),
      },
    ]
  : []),
```

Update the `SkeletonTable` columns count (line 89) from `6` to `hasCadvisor ? 8 : 6`. This requires moving the `monitoring`/`hasCadvisor` hooks above the early return. Restructure: move the monitoring hooks to just after the `useSwarmResource` call and before any early returns.

- [ ] **Step 4: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 5: Commit**

```
git add frontend/src/pages/TaskList.tsx
git commit -m "feat: show per-task CPU/memory sparklines on task list page"
```

---

### Task 7: Add Gauges and Charts to Task Detail Page

**Files:**
- Modify: `frontend/src/pages/TaskDetail.tsx`

- [ ] **Step 1: Add imports**

Add imports after line 1 (note: `ErrorBoundary` is already imported at line 6 — do not add it again):
```typescript
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { MetricsPanel, ResourceGauge } from "../components/metrics";
import { escapePromQL } from "../lib/utils";
import { formatBytes } from "../lib/formatBytes";
```

- [ ] **Step 2: Add hook calls**

Inside `TaskDetail()`, after `const [error, setError] = ...` (line 18), add:

```typescript
const monitoring = useMonitoringStatus();
const hasCadvisor = !!monitoring?.cadvisor?.activeTargets;
const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
const taskMetrics = useTaskMetrics(
  id ? `container_label_com_docker_swarm_task_id="${escapePromQL(id)}"` : "",
  hasCadvisor && !!id && task?.Status.State === "running",
);
const myMetrics = id ? taskMetrics.get(id) : undefined;
```

- [ ] **Step 3: Add gauges section**

After the error message section and before the log viewer (between line 95 and line 97), add:

```tsx
{hasCadvisor && task.Status.State === "running" && myMetrics && (
  <div className="mb-6 flex items-center justify-center gap-8">
    <ResourceGauge
      label="CPU"
      value={myMetrics.currentCpu}
      subtitle={myMetrics.currentCpu != null ? `${myMetrics.currentCpu.toFixed(1)}%` : undefined}
    />
    <ResourceGauge
      label="Memory"
      value={null}
      subtitle={myMetrics.currentMemory != null ? formatBytes(myMetrics.currentMemory) : undefined}
    />
  </div>
)}
```

Note: The CPU gauge passes `currentCpu` directly as a 0-100% value. The memory gauge passes `null` for the percentage (the `Task` type does not carry resource limits — those live on the `Service` type). The subtitle shows the absolute memory value via `formatBytes`.

- [ ] **Step 4: Add metrics charts**

After the gauges section, add:

```tsx
{hasPrometheus && task.Status.State === "running" && (
  <ErrorBoundary inline>
    <MetricsPanel
      header="Task Metrics"
      charts={[
        {
          title: "CPU Usage",
          query: `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_task_id="${escapePromQL(id!)}"}[5m])) * 100`,
          unit: "%",
          yMin: 0,
        },
        {
          title: "Memory Usage",
          query: `sum(container_memory_usage_bytes{container_label_com_docker_swarm_task_id="${escapePromQL(id!)}"})`,
          unit: "bytes",
          yMin: 0,
          color: "#34d399",
        },
      ]}
    />
  </ErrorBoundary>
)}
```

- [ ] **Step 5: Verify it compiles**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 6: Commit**

```
git add frontend/src/pages/TaskDetail.tsx
git commit -m "feat: add resource gauges and metrics charts to task detail"
```

---

### Task 8: Verify End-to-End

- [ ] **Step 1: Run lint**

Run: `cd frontend && npm run lint`
Expected: no errors

- [ ] **Step 2: Run format check**

Run: `cd frontend && npm run fmt:check`
Expected: no errors (fix with `npm run fmt` if needed)

- [ ] **Step 3: Run type check**

Run: `cd frontend && npx tsc -b --noEmit`
Expected: no errors

- [ ] **Step 4: Run tests**

Run: `cd frontend && npx vitest run`
Expected: all tests pass

- [ ] **Step 5: Build frontend**

Run: `cd frontend && npm run build`
Expected: successful build

- [ ] **Step 6: Build Go binary**

Run: `go build -o cetacean .`
Expected: successful build

- [ ] **Step 7: Fix any issues, commit**

If any step above failed, fix the issue and re-run. Commit fixes as needed.
