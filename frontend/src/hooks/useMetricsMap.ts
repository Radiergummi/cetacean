import { api } from "../api/client";
import { parseInstant, parseRange } from "../lib/prometheusParser";
import { keepPreviousData, useQuery } from "@tanstack/react-query";

interface InstantField<T> {
  query: string;
  assign: (metrics: T, value: number) => void;
}

interface RangeField<T> {
  query: string;
  assign: (metrics: T, values: number[]) => void;
}

// One instance, so a consumer memoising on the result does not rebuild on every
// render while the query is disabled or pending.
const noMetrics: Record<string, never> = {};
const noResult = { metrics: noMetrics, error: null };

export interface MetricsMapResult<T> {
  metrics: Record<string, T>;
  /** The first query that failed on the last poll; the rest still fill `metrics`. */
  error: Error | null;
}

export interface MetricsMapSpec<T> {
  /**
   * Several labels file each series under their values joined by `/`.
   */
  labelKey: string | readonly string[];
  empty: () => T;
  instant?: readonly InstantField<T>[] | undefined;
  range?: readonly RangeField<T>[] | undefined;
}

/**
 * Fetches instant and range Prometheus queries, parses results keyed by a
 * label, and returns a Record<string, T> that updates on an interval.
 *
 * useNodeMetrics, useServiceMetrics, and useTaskMetrics are thin wrappers around this.
 */
export function useMetricsMap<T>(
  cacheKey: string,
  spec: MetricsMapSpec<T>,
  enabled: boolean,
  refreshInterval = 30_000,
  step = 120,
): Record<string, T> {
  return useMetricsMapResult(cacheKey, spec, enabled, refreshInterval, step).metrics;
}

/**
 * As useMetricsMap, for a page that says when its metrics failed to load.
 */
export function useMetricsMapResult<T>(
  cacheKey: string,
  spec: MetricsMapSpec<T>,
  enabled: boolean,
  refreshInterval = 30_000,
  step = 120,
): MetricsMapResult<T> {
  const { data } = useQuery({
    queryKey: ["metrics-map", cacheKey],
    queryFn: async (): Promise<MetricsMapResult<T>> => {
      let failure: Error | null = null;
      const record = (error: unknown) => {
        console.warn(error);
        failure ??= error instanceof Error ? error : new Error(String(error));

        return null;
      };
      const now = Math.floor(Date.now() / 1000);
      const start = now - 3600;
      const [instantResponses, rangeResponses] = await Promise.all([
        Promise.all((spec.instant ?? []).map(({ query }) => api.metricsQuery(query).catch(record))),
        Promise.all(
          (spec.range ?? []).map(({ query }) =>
            api.metricsQueryRange(query, String(start), String(now), String(step)).catch(record),
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
        const field = spec.instant?.[index];

        parseInstant(response, spec.labelKey)?.forEach(([key, value]) => {
          field?.assign(ensure(key), value);
        });
      });

      rangeResponses.forEach((response, index) => {
        const field = spec.range?.[index];

        parseRange(response, spec.labelKey)?.forEach(([key, values]) => {
          field?.assign(ensure(key), values);
        });
      });

      return { metrics: map, error: failure };
    },
    enabled,
    placeholderData: keepPreviousData,
    refetchInterval: refreshInterval,
    staleTime: refreshInterval,
  });

  // keepPreviousData would hand a disabled query the last answer of another key.
  return (enabled && data) || (noResult as MetricsMapResult<T>);
}
