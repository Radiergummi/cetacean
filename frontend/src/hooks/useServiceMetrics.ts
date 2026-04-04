import { useMetricsMap } from "./useMetricsMap";
import { isCadvisorReady, useMonitoringStatus } from "./useMonitoringStatus";
import { useCallback } from "react";

export interface ServiceMetrics {
  cpu: number | null;
  cpuHistory: number[];
  memory: number | null;
  memoryHistory: number[];
}

const emptyMetrics: ServiceMetrics = {
  cpu: null,
  cpuHistory: [],
  memory: null,
  memoryHistory: [],
};

const serviceLabel = "container_label_com_docker_swarm_service_name";
const cpuQuery = `sum by (${serviceLabel})(rate(container_cpu_usage_seconds_total{${serviceLabel}!=""}[5m])) * 100`;
const memQuery = `sum by (${serviceLabel})(container_memory_usage_bytes{${serviceLabel}!=""})`;

const spec = {
  labelKey: serviceLabel,
  empty: (): ServiceMetrics => ({ ...emptyMetrics, cpuHistory: [], memoryHistory: [] }),
  instant: [
    {
      query: cpuQuery,
      assign: (m: ServiceMetrics, v: number) => {
        m.cpu = v;
      },
    },
    {
      query: memQuery,
      assign: (m: ServiceMetrics, v: number) => {
        m.memory = v;
      },
    },
  ],
  range: [
    {
      query: cpuQuery,
      assign: (m: ServiceMetrics, v: number[]) => {
        m.cpuHistory = v;
      },
    },
    {
      query: memQuery,
      assign: (m: ServiceMetrics, v: number[]) => {
        m.memoryHistory = v;
      },
    },
  ],
} as const;

export function useServiceMetrics() {
  const monitoring = useMonitoringStatus();
  const hasCadvisor = isCadvisorReady(monitoring);
  const byService = useMetricsMap("service-metrics", spec, hasCadvisor);

  const getForService = useCallback(
    (serviceName: string): ServiceMetrics => byService[serviceName] ?? emptyMetrics,
    [byService],
  );

  return { getForService, hasData: Object.keys(byService).length > 0 };
}
