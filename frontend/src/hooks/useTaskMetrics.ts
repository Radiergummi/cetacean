import { useMetricsMap, type MetricsMapSpec } from "./useMetricsMap";

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
  return useMetricsMap(
    "task-metrics-" + filter,
    buildSpec(filter),
    enabled && !!filter,
    refreshInterval,
    60,
  );
}
