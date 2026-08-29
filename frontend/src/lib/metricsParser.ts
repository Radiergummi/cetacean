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
  colorOverride?: string | undefined,
): ParsedMetrics | null {
  const result = response.data?.result?.filter(({ values }) => values?.length);

  if (!result?.length) {
    return null;
  }

  const timestamps = (result[0]?.values ?? []).map(([value]) => Number(value));
  const labels = timestamps.map((timestamp) => new Date(timestamp * 1_000).toLocaleTimeString());
  const series = result.map(({ metric, values }, index) => ({
    label: seriesLabel(metric, result.length === 1 ? title : undefined),
    color: colorOverride ?? getChartColor(index),
    data: (values ?? []).map(([, value]) => Number(value)),
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
  if (!data) {
    return [];
  }

  if (data.resultType === "scalar" || data.resultType === "string") {
    const [timestamp, value] = data.result as unknown as [number, string];

    return timestamp == null ? [] : [{ metric: {}, value, timestamp }];
  }

  return data.result
    .map(({ metric, value, values }) => {
      const point = value ?? values?.[values.length - 1];

      if (!point) {
        return null;
      }

      return { metric: metric ?? {}, value: point[1], timestamp: point[0] };
    })
    .filter((row): row is NormalizedMetricRow => row !== null);
}

/**
 * Advances a fixed-width window by one sample: drops the oldest point and
 * appends the newest, matching each series by its label. A series the
 * response omits gets a zero so every series stays the same length.
 *
 * The title has to be the same one parseRangeResult was given: a query
 * returning a single series is labelled with the chart's title, and matching
 * without it appends a hard zero to every such chart on every streamed point.
 *
 * Returns null when the response carries nothing usable, meaning the caller
 * should keep the window it already has.
 */
export function appendMetricPoint(
  previous: ParsedMetrics,
  response: PrometheusResponse,
  title?: string | undefined,
): ParsedMetrics | null {
  const result = response.data?.result?.filter(({ value }) => value);

  if (!result?.length) {
    return null;
  }

  const timestamp = Number(result[0]?.value?.[0]);
  const timeLabel = new Date(timestamp * 1000).toLocaleTimeString();

  return {
    labels: [...previous.labels.slice(1), timeLabel],
    timestamps: [...previous.timestamps.slice(1), timestamp],
    series: previous.series.map((series) => {
      const match = result.find(
        ({ metric }) =>
          seriesLabel(metric, result.length === 1 ? title : undefined) === series.label,
      );
      const value = match ? Number(match.value?.[1]) : 0;

      return { ...series, data: [...series.data.slice(1), value] };
    }),
  };
}

/** Returns true if the series labels changed between two datasets. */
export function seriesChanged(previous: ParsedMetrics | null, next: ParsedMetrics): boolean {
  if (!previous || previous.series.length !== next.series.length) {
    return true;
  }

  return previous.series.some(({ label }, index) => label !== next.series[index]?.label);
}
