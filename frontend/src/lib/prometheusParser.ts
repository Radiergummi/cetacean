import type { PrometheusResponse } from "../api/types";

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
