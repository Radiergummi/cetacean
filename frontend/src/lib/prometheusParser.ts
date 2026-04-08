import type { PrometheusResponse } from "../api/types";
import { escapePromQL } from "./utils";

/**
 * Extracts [label, value] pairs from a Prometheus instant query response.
 */
export function parseInstant(
  response: PrometheusResponse | null,
  labelKey: string,
): [string, number][] | null {
  const results = response?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, value }) => [metric?.[labelKey] || "", Number(value?.[1])] as [string, number],
  );
}

/**
 * Extracts [label, values[]] pairs from a Prometheus range query response.
 */
export function parseRange(
  response: PrometheusResponse | null,
  labelKey: string,
): [string, number[]][] | null {
  const results = response?.data?.result;

  if (!results?.length) {
    return null;
  }

  return results.map(
    ({ metric, values }) =>
      [metric?.[labelKey] || "", (values || []).map((value) => Number(value[1]))] as [
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
