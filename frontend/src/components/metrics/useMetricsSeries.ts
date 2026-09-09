// oxlint-disable react/refs -- the props mirrored here are read from inside a
// fetch callback and a stream listener, both of which run outside React's
// render and would otherwise close over the values of the render that started
// them. This is the suppression TimeSeriesChart.tsx already carried; it moved
// with the code it covers.
import { api } from "@/api/client.ts";
import type { PrometheusResponse } from "@/api/types.ts";
import { openEventStream } from "@/lib/eventStream.ts";
import {
  appendMetricPoint,
  type ParsedMetrics,
  parseRangeResult,
  seriesChanged,
} from "@/lib/metricsParser.ts";
import { generateMockSeries } from "@/lib/mockChartData.ts";
import { getErrorMessage } from "@/lib/utils";
import { useCallback, useEffect, useRef, useState } from "react";

export type MetricsState = "loading" | "data" | "empty" | "error";

const rangeIntervals: Record<string, number> = {
  "1h": 3600,
  "6h": 21600,
  "24h": 86400,
  "7d": 604800,
};

/** Seconds in a named range, falling back to an hour for one we don't know. */
export function rangeSeconds(range: string): number {
  return rangeIntervals[range] ?? 3600;
}

/** Roughly 300 points across the window, never finer than Prometheus' scrape. */
function stepFor(seconds: number): number {
  return Math.max(Math.floor(seconds / 300), 15);
}

interface Options {
  query: string;
  range: string;
  title: string;
  unit: string | undefined;
  color: string | undefined;
  from: number | undefined;
  to: number | undefined;
  refreshKey: number | undefined;
  streaming: boolean;
  /** Called whenever a fetch or a stream frame republishes the series list. */
  onSeriesInfo?: ((series: { label: string; color: string }[]) => void) | undefined;
  /** Called when the series changed identity, so a stale isolation can be dropped. */
  onSeriesReset?: (() => void) | undefined;
}

export interface MetricsSeries {
  state: MetricsState;
  errorMessage: string;
  data: ParsedMetrics | null;
  refetch: () => void;
}

/**
 * Loads a metrics window and keeps it live.
 *
 * The window is fetched once per query/range, then a stream appends points to
 * it. Only a relative window streams: an explicit from/to is a fixed period and
 * has no future to receive.
 *
 * The stream may only open against data fetched for the *current* query, or a
 * point would be appended to the previous range's arrays. That is tracked by
 * `loadedKey` — the key a completed fetch was for — rather than by a ref, which
 * is what the previous version did and why streaming never started at all: the
 * ref was set from an effect on the arriving data, but the effect that opens the
 * stream did not depend on it, so nothing re-ran to notice the gate had opened.
 * The charts fell back silently to the panel's periodic refetch. A key works
 * where the data itself would not, because it changes when a fetch completes and
 * *not* when a streamed point extends the arrays — depending on the data would
 * tear the stream down and rebuild it on every frame it delivered.
 */
export function useMetricsSeries({
  query,
  range,
  title,
  unit,
  color,
  from,
  to,
  refreshKey,
  streaming,
  onSeriesInfo,
  onSeriesReset,
}: Options): MetricsSeries {
  const [state, setState] = useState<MetricsState>("loading");
  const [errorMessage, setErrorMessage] = useState("");
  const [data, setData] = useState<ParsedMetrics | null>(null);
  const [loadedKey, setLoadedKey] = useState<string | null>(null);

  const dataRef = useRef(data);
  dataRef.current = data;
  const titleRef = useRef(title);
  titleRef.current = title;
  const unitRef = useRef(unit);
  unitRef.current = unit;
  const colorRef = useRef(color);
  colorRef.current = color;
  const onSeriesInfoRef = useRef(onSeriesInfo);
  onSeriesInfoRef.current = onSeriesInfo;
  const onSeriesResetRef = useRef(onSeriesReset);
  onSeriesResetRef.current = onSeriesReset;

  const key = `${query}|${range}|${from ?? ""}|${to ?? ""}`;

  const publish = useCallback((parsed: ParsedMetrics) => {
    setData(parsed);
    onSeriesInfoRef.current?.(parsed.series.map(({ color, label }) => ({ color, label })));

    if (seriesChanged(dataRef.current, parsed)) {
      onSeriesResetRef.current?.();
    }

    setState("data");
  }, []);

  const fetchData = useCallback(() => {
    setState("loading");

    const seconds = rangeSeconds(range);
    const now = Math.floor(Date.now() / 1000);
    const start = from ?? now - seconds;
    const end = to ?? now;
    const step = Math.max(Math.floor((end - start) / 300), 15);
    let cancelled = false;

    api
      .metricsQueryRange(query, String(start), String(end), String(step))
      .then((response) => {
        if (cancelled) {
          return;
        }

        const parsed = parseRangeResult(response, title, color);

        if (parsed) {
          publish(parsed);
          setLoadedKey(key);

          return;
        }

        if (import.meta.env.DEV) {
          publish(generateMockSeries(title, unitRef.current, start, end, step, color));
          setLoadedKey(key);

          return;
        }

        setState("empty");
      })
      .catch((error) => {
        if (!cancelled) {
          setErrorMessage(getErrorMessage(error, "Failed to load metrics"));
          setState("error");
        }
      });

    return () => {
      cancelled = true;
    };
  }, [query, range, from, to, title, color, key, publish]);

  useEffect(() => {
    // An HTTP request is the external system this effect exists to synchronise
    // with, and announcing that it started is the loading state.
    // oxlint-disable-next-line react/set-state-in-effect -- nothing to derive during render
    const cancel = fetchData();

    return () => {
      cancel?.();
    };
    // `refreshKey` exists only to be changed: the panel's refresh button bumps
    // it to force a refetch of an unchanged query. Nothing reads it — the point.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies -- re-run trigger
  }, [fetchData, refreshKey]);

  // Reopened on demand: closing the stream when the tab hides frees a
  // connection slot nobody is watching, and showing it again needs a fresh one.
  const [streamKey, setStreamKey] = useState(0);
  const fetchDataRef = useRef(fetchData);
  fetchDataRef.current = fetchData;

  const live = loadedKey === key && from == null && to == null && streaming;

  useEffect(() => {
    if (!live) {
      return;
    }

    const seconds = rangeSeconds(range);
    const url = api.metricsStreamURL(query, stepFor(seconds), seconds);

    const handleInitial = (event: MessageEvent) => {
      try {
        const parsed = parseRangeResult(
          JSON.parse(event.data) as PrometheusResponse,
          titleRef.current,
          colorRef.current,
        );

        if (parsed) {
          publish(parsed);
        }
      } catch {
        /* ignore parse errors */
      }
    };

    const handlePoint = (event: MessageEvent) => {
      try {
        const response = JSON.parse(event.data) as PrometheusResponse;

        setData((previous) =>
          previous
            ? (appendMetricPoint(previous, response, titleRef.current) ?? previous)
            : previous,
        );
      } catch {
        /* ignore parse errors */
      }
    };

    const handleQueryError = (event: MessageEvent) => {
      console.warn("[metrics stream] Prometheus error:", event.data); // eslint-disable-line no-console
    };

    const stream = openEventStream(url, {
      listeners: { initial: handleInitial, point: handlePoint, query_error: handleQueryError },
    });

    const visibilityHandler = () => {
      if (document.visibilityState === "hidden") {
        stream.close();

        return;
      }

      // Refetch rather than resume: the window moved on while the tab was
      // hidden, and the stream's own initial frame replaces it on connect.
      fetchDataRef.current();
      setStreamKey((previous) => previous + 1);
    };

    document.addEventListener("visibilitychange", visibilityHandler);

    return () => {
      stream.close();
      document.removeEventListener("visibilitychange", visibilityHandler);
    };
    // `streamKey` is bumped when a hidden tab comes back, to reopen the
    // connection this effect closed. Dropping it leaves the tab with no stream
    // until the range changes.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies -- re-run trigger
  }, [live, query, range, streamKey, publish]);

  const refetch = useCallback(() => {
    fetchDataRef.current();
  }, []);

  return { state, errorMessage, data, refetch };
}
