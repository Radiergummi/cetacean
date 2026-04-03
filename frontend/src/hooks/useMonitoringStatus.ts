import { api } from "../api/client";
import type { MonitoringStatus } from "../api/types";
import { useQuery } from "@tanstack/react-query";

export const monitoringStatusQueryKey = ["monitoring-status"] as const;

export function useMonitoringStatus(): MonitoringStatus | null {
  const { data } = useQuery({
    queryKey: monitoringStatusQueryKey,
    queryFn: () => api.monitoringStatus(),
    staleTime: 60_000,
    retry: false,
  });

  return data ?? null;
}

/**
 * Derives whether Prometheus is configured and reachable.
 */
export function isPrometheusReady(status: MonitoringStatus | null): boolean {
  return !!status?.prometheusConfigured && !!status?.prometheusReachable;
}

/**
 * Derives whether cAdvisor targets are available via Prometheus.
 */
export function isCadvisorReady(status: MonitoringStatus | null): boolean {
  return isPrometheusReady(status) && !!status?.cadvisor?.targets;
}

/**
 * Derives whether node-exporter targets are available via Prometheus.
 */
export function isNodeExporterReady(status: MonitoringStatus | null): boolean {
  return isPrometheusReady(status) && !!status?.nodeExporter?.targets;
}
