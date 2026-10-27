import { useMetricsMapResult, type MetricsMapResult, type MetricsMapSpec } from "./useMetricsMap";

export interface TaskMetricsData {
  cpu: number[];
  memory: number[];
  currentCpu: number | null;
  currentMemory: number | null;
}

const taskIdLabel = "container_label_com_docker_swarm_task_id";

/** One CPU and one memory query per filter; their series merge into one map. */
function buildSpec(filters: readonly string[]): MetricsMapSpec<TaskMetricsData> {
  return {
    labelKey: taskIdLabel,
    empty: () => ({ cpu: [], memory: [], currentCpu: null, currentMemory: null }),
    range: filters.flatMap((filter) => rangeFields(filter)),
  };
}

function rangeFields(filter: string) {
  const cpuQuery = `sum by (${taskIdLabel})(rate(container_cpu_usage_seconds_total{${filter}}[5m])) * 100`;
  const memQuery = `sum by (${taskIdLabel})(container_memory_usage_bytes{${filter}})`;

  return [
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
  ];
}

export function useTaskMetrics(
  filter: string,
  enabled: boolean,
  refreshInterval = 30_000,
): Record<string, TaskMetricsData> {
  return useTaskMetricsResult(filter ? [filter] : [], enabled, refreshInterval).metrics;
}

/**
 * Task metrics across several filters, each its own pair of queries, with the
 * error of the first that failed.
 */
export function useTaskMetricsResult(
  filters: readonly string[],
  enabled: boolean,
  refreshInterval = 30_000,
): MetricsMapResult<TaskMetricsData> {
  return useMetricsMapResult(
    "task-metrics-" + filters.join(","),
    buildSpec(filters),
    enabled && filters.length > 0,
    refreshInterval,
    60,
  );
}
