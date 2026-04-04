import { api } from "../api/client";
import { parseInstant, parseRange } from "../lib/prometheusParser";
import { useQuery } from "@tanstack/react-query";

interface InstantField<T> {
  query: string;
  assign: (metrics: T, value: number) => void;
}

interface RangeField<T> {
  query: string;
  assign: (metrics: T, values: number[]) => void;
}

export interface MetricsMapSpec<T> {
  labelKey: string;
  empty: () => T;
  instant?: readonly InstantField<T>[];
  range?: readonly RangeField<T>[];
}

/**
 * Fetches instant and range Prometheus queries, parses results keyed by a
 * label, and returns a Record<string, T> that updates on an interval.
 *
 * Both useNodeMetrics and useServiceMetrics are thin wrappers around this.
 */
export function useMetricsMap<T>(
  cacheKey: string,
  spec: MetricsMapSpec<T>,
  enabled: boolean,
  refreshInterval = 30_000,
): Record<string, T> {
  const { data } = useQuery({
    queryKey: ["metrics-map", cacheKey],
    queryFn: async () => {
      const now = Math.floor(Date.now() / 1000);
      const start = now - 3600;
      const step = 120;

      const [instantResponses, rangeResponses] = await Promise.all([
        Promise.all(
          (spec.instant ?? []).map(({ query }) =>
            api.metricsQuery(query).catch((error) => {
              console.warn(error);
              return null;
            }),
          ),
        ),
        Promise.all(
          (spec.range ?? []).map(({ query }) =>
            api
              .metricsQueryRange(query, String(start), String(now), String(step))
              .catch((error) => {
                console.warn(error);
                return null;
              }),
          ),
        ),
      ]);

      const map: Record<string, T> = {};

      const ensure = (key: string) => {
        if (!map[key]) {
          map[key] = spec.empty();
        }

        return map[key];
      };

      instantResponses.forEach((response, index) => {
        const field = spec.instant![index];
        parseInstant(response, spec.labelKey)?.forEach(([key, value]) => {
          field.assign(ensure(key), value);
        });
      });

      rangeResponses.forEach((response, index) => {
        const field = spec.range![index];
        parseRange(response, spec.labelKey)?.forEach(([key, values]) => {
          field.assign(ensure(key), values);
        });
      });

      return map;
    },
    enabled,
    refetchInterval: refreshInterval,
    refetchIntervalInBackground: true,
    staleTime: refreshInterval,
  });

  return data ?? {};
}
