import type { PrometheusResponse } from "../api/types";
import { escapePromQL } from "./utils";

/**
 * The key a series keyed by several labels is filed under.
 */
export function joinLabelValues(values: readonly string[]): string {
  return values.join("/");
}

/**
 * The key a series is filed under: one label's value, or several joined.
 */
function seriesKey(
  metric: Record<string, string> | undefined,
  labelKey: string | readonly string[],
): string {
  const labels = typeof labelKey === "string" ? [labelKey] : labelKey;

  return joinLabelValues(labels.map((label) => metric?.[label] ?? ""));
}

/**
 * Extracts [label, value] pairs from a Prometheus instant query response.
 */
export function parseInstant(
  response: PrometheusResponse | null,
  labelKey: string | readonly string[],
): [string, number][] | null {
  const results = response?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, value }) => [seriesKey(metric, labelKey), Number(value?.[1])] as [string, number],
  );
}

/**
 * Extracts [label, values[]] pairs from a Prometheus range query response.
 */
export function parseRange(
  response: PrometheusResponse | null,
  labelKey: string | readonly string[],
): [string, number[]][] | null {
  const results = response?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, values }) =>
      [seriesKey(metric, labelKey), (values || []).map((value) => Number(value[1]))] as [
        string,
        number[],
      ],
  );
}

/**
 * Build a Prometheus instance label selector from node identification fields.
 * Tries exact instance match first, then address with port wildcard,
 * then hostname with optional FQDN suffix.
 */
export function buildInstanceFilter(instance: string, address: string, hostname: string): string {
  if (instance) {
    return `instance="${escapePromQL(instance)}"`;
  }

  if (address) {
    return `instance=~"${escapePromQL(address)}:.*"`;
  }

  if (hostname) {
    return `instance=~"${escapePromQL(hostname)}(\\..+)?:.*"`;
  }

  return "";
}
