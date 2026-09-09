import { useChartSync } from "./ChartSyncProvider";
import type { ParsedMetrics } from "@/lib/metricsParser.ts";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

interface Options {
  chartId: string;
  data: ParsedMetrics | null;
  /** Present when the caller owns the isolation; absent when the chart does. */
  isolatedLabel?: string | null | undefined;
  onIsolationChange?: ((label: string | null) => void) | undefined;
}

export interface SeriesIsolation {
  /** Index of the isolated series, or null when every series is drawn. */
  index: number | null;
  /** Isolate by index, publishing to sibling charts. */
  isolate: (index: number | null) => void;
  /** Drop the isolation without publishing — for when the series list changes. */
  clear: () => void;
}

/**
 * Which series is isolated, whether this chart or its caller decides.
 *
 * Isolation is held by *index* here because that is what a click on a dataset
 * gives, but travels between charts by *label*, since sibling charts plot
 * different metrics and share only the series names. The two representations
 * are reconciled against the data on every read, which is also what makes a
 * label naming a series this chart does not have resolve to no isolation rather
 * than to whatever sits at that index.
 */
export function useSeriesIsolation({
  chartId,
  data,
  isolatedLabel,
  onIsolationChange,
}: Options): SeriesIsolation {
  const sync = useChartSync();
  const controlled = isolatedLabel !== undefined;
  const [localIndex, setLocalIndex] = useState<number | null>(null);

  const dataRef = useRef(data);
  dataRef.current = data;

  const controlledIndex = useMemo(() => {
    if (!controlled || isolatedLabel == null || !data) {
      return null;
    }

    const index = data.series.findIndex(({ label }) => label === isolatedLabel);

    return index >= 0 ? index : null;
  }, [controlled, isolatedLabel, data]);

  const index = controlled ? controlledIndex : localIndex;

  const set = useCallback(
    (next: number | null) => {
      if (controlled) {
        onIsolationChange?.(next != null ? (dataRef.current?.series[next]?.label ?? null) : null);

        return;
      }

      setLocalIndex(next);
    },
    [controlled, onIsolationChange],
  );

  useEffect(() => {
    return sync.subscribeIsolation(chartId, (label) => {
      const current = dataRef.current;

      if (!current || label == null) {
        set(null);

        return;
      }

      const found = current.series.findIndex((series) => series.label === label);

      set(found >= 0 ? found : null);
    });
  }, [chartId, sync, set]);

  const isolate = useCallback(
    (next: number | null) => {
      set(next);
      sync.publishIsolation(
        chartId,
        next != null ? (dataRef.current?.series[next]?.label ?? null) : null,
      );
    },
    [chartId, set, sync],
  );

  const clear = useCallback(() => {
    set(null);
  }, [set]);

  return { index, isolate, clear };
}
