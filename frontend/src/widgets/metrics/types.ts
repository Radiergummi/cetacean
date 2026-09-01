/**
 * The `get_metrics` structured output, mirroring internal/mcp.metricsResult.
 *
 * The widget is built and shipped separately from the server and cannot be
 * type-checked against it, so this is the contract between the two.
 */
export interface MetricsResult {
  target: string;
  id: string;
  name: string;
  metric: string;
  unit: string;
  range: string;
  series: MetricSeries[];
}

export interface MetricSeries {
  name: string;
  points: MetricPoint[];
}

export interface MetricPoint {
  /** RFC 3339, as the tool reports it. */
  time: string;
  value: number;
}

/** The windows get_metrics accepts, in the order they are offered. */
export const metricRanges = ["1h", "6h", "24h", "7d"] as const;

export type MetricRange = (typeof metricRanges)[number];
