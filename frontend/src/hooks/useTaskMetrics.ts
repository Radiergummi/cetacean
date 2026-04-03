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
  const { data } = useQuery({
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

        const values = series.values.map(([, value]) => parseFloat(value));

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

        const values = series.values.map(([, value]) => parseFloat(value));
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
    refetchIntervalInBackground: true,
    staleTime: refreshInterval,
    retry: false,
  });

  return data ?? emptyMap;
}
