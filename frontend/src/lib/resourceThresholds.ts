import type { Service } from "@/api/types";
import type { Threshold } from "@/components/metrics";
import { getSemanticChartColor } from "@/lib/chartColors";

/**
 * Build chart threshold lines for a service's CPU resource reservations and limits.
 * Values are converted from NanoCPUs to percentage of 1 vCPU.
 */
export function cpuThresholds(service: Service): Threshold[] {
  const resources = service.Spec.TaskTemplate?.Resources;

  if (!resources) {
    return [];
  }

  const out: Threshold[] = [];

  if (resources.Reservations?.NanoCPUs) {
    const value = (resources.Reservations.NanoCPUs / 1e9) * 100;

    out.push({
      label: "Reserved",
      value,
      color: getSemanticChartColor("reserved"),
      dash: [12, 6],
    });
  }

  if (resources.Limits?.NanoCPUs) {
    const value = (resources.Limits.NanoCPUs / 1e9) * 100;

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
 * Values are raw bytes.
 */
export function memoryThresholds(service: Service): Threshold[] {
  const resources = service.Spec.TaskTemplate?.Resources;

  if (!resources) {
    return [];
  }

  const out: Threshold[] = [];

  if (resources.Reservations?.MemoryBytes) {
    out.push({
      label: "Reserved",
      value: resources.Reservations.MemoryBytes,
      color: getSemanticChartColor("reserved"),
      dash: [12, 6],
    });
  }

  if (resources.Limits?.MemoryBytes) {
    out.push({
      label: "Limit",
      value: resources.Limits.MemoryBytes,
      color: getSemanticChartColor("critical"),
      dash: [12, 6],
    });
  }

  return out;
}
