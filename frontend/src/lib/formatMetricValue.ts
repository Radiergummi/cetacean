import { formatBytes } from "./formatBytes";

/** Format a metric value with appropriate units for chart display. */
export function formatMetricValue(v: number, unit?: string): string {
  if (unit === "bytes" || unit === "bytes/s") {
    return formatBytes(v) + (unit === "bytes/s" ? "/s" : "");
  }
  if (unit === "%") return `${v.toFixed(1)}%`;
  if (unit === "cores") return `${v.toFixed(3)}`;
  return v.toFixed(2);
}
