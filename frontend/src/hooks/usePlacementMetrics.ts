import { joinLabelValues } from "../lib/prometheusParser";
import { useMetricsMap, type MetricsMapSpec } from "./useMetricsMap";
import { isCadvisorReady, useMonitoringStatus } from "./useMonitoringStatus";

export interface PlacementUsage {
  cpu: number | null;
  memory: number | null;
}

const serviceLabel = "container_label_com_docker_swarm_service_id";
const nodeLabel = "container_label_com_docker_swarm_node_id";
const filter = `container_label_com_docker_swarm_task_id!=""`;

export const placementUsageSpec: MetricsMapSpec<PlacementUsage> = {
  labelKey: [serviceLabel, nodeLabel],
  empty: () => ({ cpu: null, memory: null }),
  instant: [
    {
      query: `sum by (${serviceLabel}, ${nodeLabel})(rate(container_cpu_usage_seconds_total{${filter}}[5m])) * 100`,
      assign: (usage, value) => {
        usage.cpu = value;
      },
    },
    {
      query: `sum by (${serviceLabel}, ${nodeLabel})(container_memory_usage_bytes{${filter}})`,
      assign: (usage, value) => {
        usage.memory = value;
      },
    },
  ],
};

/**
 * The key one service's usage on one node is filed under.
 */
export function placementUsageKey(serviceId: string, nodeId: string): string {
  return joinLabelValues([serviceId, nodeId]);
}

/**
 * Each service's CPU and memory on each node; empty without cAdvisor.
 */
export function usePlacementMetrics(): Record<string, PlacementUsage> {
  const hasCadvisor = isCadvisorReady(useMonitoringStatus());

  return useMetricsMap("placement-metrics", placementUsageSpec, hasCadvisor);
}
