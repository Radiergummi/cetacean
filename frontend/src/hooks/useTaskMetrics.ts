import { useMetricsMapsResult, type MetricsMapResult, type MetricsMapSpec } from "./useMetricsMap";
import { useMemo } from "react";

export interface TaskMetricsData {
  cpu: number[];
  memory: number[];
  currentCpu: number | null;
  currentMemory: number | null;
}

const taskIdLabel = "container_label_com_docker_swarm_task_id";

function buildSpec(filter: string): MetricsMapSpec<TaskMetricsData> {
  const cpuQuery = `sum by (${taskIdLabel})(rate(container_cpu_usage_seconds_total{${filter}}[5m])) * 100`;
  const memQuery = `sum by (${taskIdLabel})(container_memory_usage_bytes{${filter}})`;

  return {
    labelKey: taskIdLabel,
    empty: () => ({ cpu: [], memory: [], currentCpu: null, currentMemory: null }),
    range: [
      {
        query: cpuQuery,
        assign: (m: TaskMetricsData, v: number[]) => {
          m.cpu = v;
          m.currentCpu = v[v.length - 1] ?? null;
        },
      },
      {
        query: memQuery,
        assign: (m: TaskMetricsData, v: number[]) => {
          m.memory = v;
          m.currentMemory = v[v.length - 1] ?? null;
        },
      },
    ],
  };
}

export function useTaskMetrics(
  filter: string,
  enabled: boolean,
  refreshInterval = 30_000,
): Record<string, TaskMetricsData> {
  const filters = useMemo(() => (filter ? [filter] : []), [filter]);

  return useTaskMetricsResult(filters, enabled, refreshInterval).metrics;
}

/**
 * Task metrics across several filters, each its own cached pair of queries,
 * with the error of the first that failed.
 */
export function useTaskMetricsResult(
  filters: readonly string[],
  enabled: boolean,
  refreshInterval = 30_000,
): MetricsMapResult<TaskMetricsData> {
  const entries = useMemo(
    () =>
      filters.map((filter) => ({ cacheKey: `task-metrics-${filter}`, spec: buildSpec(filter) })),
    [filters],
  );

  return useMetricsMapsResult(entries, enabled && filters.length > 0, refreshInterval, 60);
}
