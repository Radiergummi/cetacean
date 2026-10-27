import { api } from "../api/client";
import { parseInstant, parseRange } from "../lib/prometheusParser";
import { keepPreviousData, useQueries, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";

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

/** One cached query in a set of metrics maps. */
export interface MetricsMapEntry<T> {
  cacheKey: string;
  spec: MetricsMapSpec<T>;
}

async function fetchMetricsMap<T>(
  spec: MetricsMapSpec<T>,
  step: number,
): Promise<MetricsMapResult<T>> {
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
  return useMetricsMapsResult([{ cacheKey, spec }], enabled, refreshInterval, step);
}

/**
 * Several metrics maps merged into one, each its own cached query: changing
 * one entry refetches that entry alone.
 */
export function useMetricsMapsResult<T>(
  entries: readonly MetricsMapEntry<T>[],
  enabled: boolean,
  refreshInterval = 30_000,
  step = 120,
): MetricsMapResult<T> {
  // keepPreviousData would hand a disabled query the last answer of another key.
  const combine = useCallback(
    (results: UseQueryResult<MetricsMapResult<T>>[]) => {
      const answers = enabled ? results.flatMap(({ data }) => (data ? [data] : [])) : [];

      return {
        result: merge(answers),
        settled: answers.length === results.length,
      };
    },
    [enabled],
  );

  const { result, settled } = useQueries({
    queries: entries.map(({ cacheKey, spec }) => ({
      queryKey: ["metrics-map", cacheKey],
      queryFn: () => fetchMetricsMap(spec, step),
      enabled,
      placeholderData: keepPreviousData,
      refetchInterval: refreshInterval,
      staleTime: refreshInterval,
    })),
    combine,
  });

  // A changed entry is a new query with no answer yet; until it lands, the last
  // full answer fills in beneath, as keepPreviousData does for a single query.
  const [held, setHeld] = useState<MetricsMapResult<T>>(noResult as MetricsMapResult<T>);

  if ((settled || !enabled) && held !== result) {
    setHeld(result);
  }

  return useMemo(
    () => (settled || !enabled ? result : merge([held, result])),
    [settled, enabled, result, held],
  );
}

function merge<T>(answers: readonly MetricsMapResult<T>[]): MetricsMapResult<T> {
  if (answers.length <= 1) {
    return answers[0] ?? (noResult as MetricsMapResult<T>);
  }

  return {
    metrics: Object.assign({}, ...answers.map(({ metrics }) => metrics)),
    error: answers.find(({ error }) => error)?.error ?? null,
  };
}
