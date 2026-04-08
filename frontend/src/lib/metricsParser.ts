import type { PrometheusResponse } from "@/api/types.ts";
import { getChartColor } from "@/lib/chartColors.ts";

export interface ParsedMetrics {
  labels: string[];
  timestamps: number[];
  series: {
    label: string;
    color: string;
    data: number[];
  }[];
}

export function seriesLabel(metric: Record<string, string> | undefined, fallback?: string): string {
  if (!metric) {
    return fallback ?? "value";
  }

  const { __name__, ...labels } = metric;
  const labelStr = Object.values(labels).filter(Boolean).join(", ");

  if (labelStr) {
    return labelStr;
  }

  if (__name__) {
    return __name__;
  }

  return fallback ?? "value";
}

/** Parse a Prometheus range query response into chart-ready data. */
export function parseRangeResult(
  response: PrometheusResponse,
  title: string,
  colorOverride?: string,
): ParsedMetrics | null {
  if (!response.data?.result?.length) {
    return null;
  }

  const result = response.data.result;
  const timestamps = result[0].values!.map(([value]) => Number(value));
  const labels = timestamps.map((timestamp) => new Date(timestamp * 1_000).toLocaleTimeString());
  const series = result.map(({ metric, values }, index) => ({
    label: seriesLabel(metric, result.length === 1 ? title : undefined),
    color: colorOverride ?? getChartColor(index),
    data: values!.map(([, value]) => Number(value)),
  }));

  return { labels, timestamps, series };
}

/**
 * Format a metric as a full PromQL-style identifier, e.g. `cpu_usage{instance="node1", job="cadvisor"}`.
 */
export function formatMetricIdentifier(metric: Record<string, string>): string {
  const name = metric["__name__"] ?? "";
  const labels = Object.entries(metric)
    .filter(([key]) => key !== "__name__")
    .map(([key, value]) => `${key}="${value}"`)
    .join(", ");

  if (!name && !labels) {
    return "{}";
  }

  if (!labels) {
    return name;
  }

  return `${name}{${labels}}`;
}

export interface NormalizedMetricRow {
  metric: Record<string, string>;
  value: string;
  timestamp: number;
}

/**
 * Normalize a Prometheus result array into flat rows.
 * For vector results each row is one series; for matrix results the last value is used.
 */
export function normalizePrometheusRows(data: PrometheusResponse["data"]): NormalizedMetricRow[] {
  return data.result
    .map(({ metric, value, values }) => {
      const point = value ?? values?.[values.length - 1];

      if (!point) {
        return null;
      }

      return { metric, value: point[1], timestamp: point[0] };
    })
    .filter((row): row is NormalizedMetricRow => row !== null);
}

/** Returns true if the series labels changed between two datasets. */
export function seriesChanged(previous: ParsedMetrics | null, next: ParsedMetrics): boolean {
  if (!previous || previous.series.length !== next.series.length) {
    return true;
  }

  return previous.series.some(({ label }, index) => label !== next.series[index].label);
}
