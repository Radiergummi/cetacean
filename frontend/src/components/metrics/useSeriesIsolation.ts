// oxlint-disable react/refs -- `dataRef` is read from the cross-chart sync
// subscription and from the isolate callback, both of which run outside render.
// Depending on `data` instead would resubscribe on every streamed point.
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
}

/**
 * Which series is isolated, whether this chart or its caller decides.
 *
 * The isolation is a *label* in both modes, and the index a click gives is
 * resolved back to one immediately. Holding an index instead — as the
 * uncontrolled mode used to — meant the isolation silently changed meaning
 * whenever the series list did, so the fetch had to reach back in and clear it
 * through a ref assigned during render. A label needs no such telling: it is
 * matched against the series actually plotted on every read, so one that has
 * gone resolves to no isolation rather than to whatever moved into its slot.
 *
 * A label is also what travels between charts, since siblings plot different
 * metrics and share only the series names.
 */
export function useSeriesIsolation({
  chartId,
  data,
  isolatedLabel,
  onIsolationChange,
}: Options): SeriesIsolation {
  const sync = useChartSync();
  const controlled = isolatedLabel !== undefined;
  const [localLabel, setLocalLabel] = useState<string | null>(null);

  const dataRef = useRef(data);
  dataRef.current = data;

  const label = controlled ? (isolatedLabel ?? null) : localLabel;

  const index = useMemo(() => {
    if (label == null || !data) {
      return null;
    }

    const found = data.series.findIndex((series) => series.label === label);

    return found >= 0 ? found : null;
  }, [label, data]);

  const set = useCallback(
    (next: string | null) => {
      if (controlled) {
        onIsolationChange?.(next);

        return;
      }

      setLocalLabel(next);
    },
    [controlled, onIsolationChange],
  );

  useEffect(() => {
    return sync.subscribeIsolation(chartId, (published) => {
      // A sibling plots different metrics, so it can name a series this chart
      // does not have. That is no isolation here rather than someone else's.
      const known =
        published != null && dataRef.current?.series.some(({ label }) => label === published);

      set(known ? published : null);
    });
  }, [chartId, sync, set]);

  const isolate = useCallback(
    (next: number | null) => {
      const nextLabel = next != null ? (dataRef.current?.series[next]?.label ?? null) : null;

      set(nextLabel);
      sync.publishIsolation(chartId, nextLabel);
    },
    [chartId, set, sync],
  );

  return { index, isolate };
}
