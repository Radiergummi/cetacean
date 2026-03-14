# Per-Task Resource Usage Metrics

## Goal

Show per-task CPU and memory usage in all task tables (service detail, node detail, task list page) as inline sparklines, and on the task detail page as gauges + time-series charts.

## Background

Swarmpit shows live CPU% and memory per task in its task tables. Cetacean currently shows per-service and per-node metrics via Prometheus/cAdvisor but has no per-task breakdown. cAdvisor already exposes `container_label_com_docker_swarm_task_id` labels on all container metrics, so the data is available — we just need to query and render it.

## Approach: Batch Query Per Table

One `query_range` call per table, grouped by task ID. For example, a service detail page fires:

```promql
sum by (container_label_com_docker_swarm_task_id)(
  rate(container_cpu_usage_seconds_total{
    container_label_com_docker_swarm_service_name="SERVICE_NAME"
  }[5m])
) * 100
```

This returns all tasks' time series in a single response. The frontend distributes series to matching table rows by task ID. Two queries per table (CPU + memory), not two per row.

### Query Filters by Context

| Context | Filter |
|---|---|
| Service detail | `container_label_com_docker_swarm_service_name="SERVICE_NAME"` |
| Node detail | `instance=~"HOSTNAME:8080"` (cAdvisor instance for the node) |
| Task list page | No additional filter (all running tasks) |
| Task detail page | `container_label_com_docker_swarm_task_id="TASK_ID"` |

### Query Parameters

- Time range: 1h for table sparklines, configurable (1h/6h/24h/7d) for task detail charts
- Step: 60s for sparklines (60 data points), standard step for detail charts
- Auto-refresh: every 30s

## New Files

### `frontend/src/hooks/useTaskMetrics.ts`

Hook that batch-fetches per-task CPU and memory metrics.

```typescript
function useTaskMetrics(
  filter: string,       // Prometheus label filter (e.g. container_label_com_docker_swarm_service_name="foo")
  taskIds: string[],    // Running task IDs to match against results
  options?: { range?: string; refreshInterval?: number }
): {
  metrics: Map<string, TaskMetricsData>;  // taskId -> { cpu, memory }
  loading: boolean;
  error: Error | null;
}

interface TaskMetricsData {
  cpu: Array<[number, number]>;     // [timestamp, percent]
  memory: Array<[number, number]>;  // [timestamp, bytes]
  currentCpu: number | null;        // latest CPU %
  currentMemory: number | null;     // latest memory bytes
}
```

Behavior:
- Fires 2 `api.metricsQueryRange()` calls (CPU + memory)
- Extracts `container_label_com_docker_swarm_task_id` from each result series
- Builds a Map keyed by task ID
- Skips queries when `taskIds` is empty or Prometheus is not configured
- Auto-refreshes on the configured interval (default 30s)
- Returns stable references (memoized map) to avoid unnecessary re-renders

### `frontend/src/components/metrics/TaskSparkline.tsx`

Inline SVG sparkline for table cells.

```typescript
function TaskSparkline(props: {
  data: Array<[number, number]> | null;  // time series points
  currentValue: number | null;           // latest value to display as text
  type: "cpu" | "memory";               // determines formatting + color
}): JSX.Element
```

Rendering:
- SVG element, ~80px wide, ~24px tall
- Line-only polyline, no axes/gridlines/labels
- Current value displayed as text to the right of the sparkline
  - CPU: formatted as percentage (e.g. "0.3%")
  - Memory: formatted with `formatBytes` (e.g. "1.4 GB")
- Color: blue for CPU, a distinct color for memory (matching existing chart conventions)
- Hover tooltip showing value + timestamp at the hovered point
- When `data` is null or empty: render "—" (no data state)
- Subtle skeleton placeholder while loading

## Modified Files

### `frontend/src/components/TasksTable.tsx`

Add CPU and Memory sparkline columns to the task table.

Changes:
- Accept an optional `metrics` prop: `Map<string, TaskMetricsData> | undefined`
- Add two columns after "State": "CPU" and "Memory"
- Each cell renders `<TaskSparkline>` with data from the metrics map, looked up by `task.ID`
- Columns hidden when `metrics` is undefined (Prometheus not configured)
- Shutdown/failed tasks show "—"

### `frontend/src/pages/TaskList.tsx`

Add sparkline columns to the global task list DataTable.

Changes:
- Call `useTaskMetrics("")` with no filter to get all running task metrics
- Pass `taskIds` of currently visible running tasks
- Add CPU and Memory column definitions to the DataTable config
- Each column renders `<TaskSparkline>` with data from the metrics map

### `frontend/src/pages/ServiceDetail.tsx`

Wire up `useTaskMetrics` and pass to `TasksTable`.

Changes:
- Call `useTaskMetrics(filter, runningTaskIds)` where filter is `container_label_com_docker_swarm_service_name="${serviceName}"`
- Extract running task IDs from the tasks list
- Pass the `metrics` map to `<TasksTable>`

### `frontend/src/pages/NodeDetail.tsx`

Wire up `useTaskMetrics` and pass to `TasksTable`.

Changes:
- Call `useTaskMetrics(filter, runningTaskIds)` where filter targets the node's cAdvisor instance
- Pass the `metrics` map to `<TasksTable>`

### `frontend/src/pages/TaskDetail.tsx`

Add gauges and time-series charts for the single task.

Changes:
- Add two `ResourceGauge` components (existing component) for current CPU and memory
  - CPU gauge: current usage vs. service resource limit (if set), or absolute value
  - Memory gauge: current usage vs. memory limit (if set)
- Add two `TimeSeriesChart` panels (existing component) for CPU and memory history
  - Use the standard MetricsPanel range selector (1h/6h/24h/7d)
  - Filter: `container_label_com_docker_swarm_task_id="TASK_ID"`
- Section hidden when Prometheus is not configured

## Graceful Degradation

- **Prometheus not configured**: sparkline columns hidden from tables, gauges/charts not rendered on task detail. Uses existing `useMonitoringStatus` hook.
- **Shutdown/failed tasks**: sparkline shows "—" (Prometheus returns no data for stopped containers)
- **Loading state**: skeleton placeholder in table cells
- **cAdvisor not running**: same as Prometheus not configured — queries return empty results

## No Backend Changes

The existing Prometheus proxy (`/-/metrics/query_range`) and cAdvisor container labels provide everything needed. No new Go code, no API changes, no cache modifications.
