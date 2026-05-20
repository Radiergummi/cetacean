import type { Service } from "@/api/types";
import type { Threshold } from "@/components/metrics";
import { getSemanticChartColor } from "@/lib/chartColors";

/**
 * Build chart threshold lines for a service's CPU resource reservations and limits.
 * Values are converted from NanoCPUs to percentage of 1 vCPU, then multiplied by
 * `replicas` so they match the service-wide `sum(...)` query plotted on the chart.
 * Without this multiplication a 3-replica service with a 1-core/task limit shows
 * a "Limit" line at 100% while the actual usage line sums to 300%.
 */
export function cpuThresholds(service: Service, replicas = 1): Threshold[] {
  const resources = service.Spec.TaskTemplate?.Resources;

  if (!resources || replicas <= 0) {
    return [];
  }

  const out: Threshold[] = [];

  if (resources.Reservations?.NanoCPUs) {
    const value = (resources.Reservations.NanoCPUs / 1e9) * 100 * replicas;

    out.push({
      label: "Reserved",
      value,
      color: getSemanticChartColor("reserved"),
      dash: [12, 6],
    });
  }

  if (resources.Limits?.NanoCPUs) {
    const value = (resources.Limits.NanoCPUs / 1e9) * 100 * replicas;

    out.push({
      label: "Limit",
      value,
      color: getSemanticChartColor("critical"),
      dash: [12, 6],
    });
  }

  return out;
}

/**
 * Build chart threshold lines for a service's memory resource reservations and limits.
 * Values are raw bytes, multiplied by `replicas` so they match the service-wide
 * `sum(container_memory_usage_bytes)` query plotted on the chart.
 */
export function memoryThresholds(service: Service, replicas = 1): Threshold[] {
  const resources = service.Spec.TaskTemplate?.Resources;

  if (!resources || replicas <= 0) {
    return [];
  }

  const out: Threshold[] = [];

  if (resources.Reservations?.MemoryBytes) {
    out.push({
      label: "Reserved",
      value: resources.Reservations.MemoryBytes * replicas,
      color: getSemanticChartColor("reserved"),
      dash: [12, 6],
    });
  }

  if (resources.Limits?.MemoryBytes) {
    out.push({
      label: "Limit",
      value: resources.Limits.MemoryBytes * replicas,
      color: getSemanticChartColor("critical"),
      dash: [12, 6],
    });
  }

  return out;
}
