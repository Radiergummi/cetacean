import { useEffect, useRef, useState } from "react";
import { api } from "../api/client";

export interface TaskMetricsData {
  cpu: number[];
  memory: number[];
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
  const intervalRef = useRef<ReturnType<typeof setInterval>>(undefined);

  useEffect(() => {
    if (!enabled) {
      setMetrics(EMPTY_MAP);
      return;
    }

    const fetchMetrics = async () => {
      const now = Math.floor(Date.now() / 1000);
      const start = String(now - 3600);
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
