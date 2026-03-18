import { useChartSync } from "./ChartSyncProvider";
import { useMetricsPanelContext } from "./MetricsPanelContext";
import { api } from "@/api/client.ts";
import type { PrometheusResponse } from "@/api/types.ts";
import { useMatchesBreakpoint } from "@/hooks/useMatchesBreakpoint.ts";
import { getChartColor, getSemanticChartColor } from "@/lib/chartColors.ts";
import { chartTooltipClasses } from "@/lib/chartTooltip.ts";
import { formatMetricValue } from "@/lib/format.ts";
import { generateMockSeries } from "@/lib/mockChartData.ts";
import { getErrorMessage } from "@/lib/utils";
import {
  CategoryScale,
  Chart as ChartJS,
  type ChartData,
  type ChartOptions,
  Filler,
  LinearScale,
  LineElement,
  type Plugin,
  PointElement,
  Tooltip as ChartTooltip,
} from "chart.js";
import zoomPlugin from "chartjs-plugin-zoom";
import { AreaChart, BarChart3, LineChart, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Line } from "react-chartjs-2";

ChartJS.register(
  LineElement,
  PointElement,
  LinearScale,
  CategoryScale,
  Filler,
  ChartTooltip,
  zoomPlugin,
);

export interface Threshold {
  label: string;
  value: number;
  color: string;
  dash?: number[];
}

interface Props {
  title: string;
  query: string;
  range: string;
  unit?: string;
  refreshKey?: number;
  thresholds?: Threshold[];
  /** Force y-axis minimum value (e.g. 0 to always start at zero). */
  yMin?: number;
  /** Override the default series color. */
  color?: string;
  from?: number;
  to?: number;
  onRangeSelect?: (from: number, to: number) => void;
  onSeriesDoubleClick?: (seriesLabel: string) => void;
  onSeriesInfo?: (series: { label: string; color: string }[]) => void;
  stackable?: boolean;
  /** Controlled isolation: label of the isolated series, or null for none. */
  isolatedLabel?: string | null;
  /** Fires when isolation changes (from chart clicks or sync). */
  onIsolationChange?: (label: string | null) => void;
}

type State = "loading" | "data" | "empty" | "error";

interface TooltipData {
  time: string;
  series: { label: string; color: string; value: string; raw: number; dashed?: boolean }[];
  x: number;
  chartWidth: number;
  top: number;
}

const rangeIntervals: Record<string, number> = {
  "1h": 3600,
  "6h": 21600,
  "24h": 86400,
  "7d": 604800,
};

const formatValue = formatMetricValue;

function seriesLabel(metric: Record<string, string> | undefined, fallback?: string): string {
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
function parseRangeResult(
  resp: PrometheusResponse,
  title: string,
  colorOverride?: string,
): FetchedData | null {
  if (!resp.data?.result?.length) {
    return null;
  }
  const result = resp.data.result;
  const timestamps = result[0].values!.map((v) => Number(v[0]));
  const labels = timestamps.map((ts) => new Date(ts * 1000).toLocaleTimeString());
  const series = result.map((s, i) => ({
    label: seriesLabel(s.metric, result.length === 1 ? title : undefined),
    color: colorOverride ?? getChartColor(i),
    data: s.values!.map((v) => Number(v[1])),
  }));
  return { labels, timestamps, series };
}

/** Create a vertical gradient fill for a series color. */
function makeGradient(
  ctx: CanvasRenderingContext2D,
  chartArea: { top: number; bottom: number },
  color: string,
) {
  const grad = ctx.createLinearGradient(0, chartArea.top, 0, chartArea.bottom);
  grad.addColorStop(0, color + "30");
  grad.addColorStop(1, color + "00");
  return grad;
}

const TOOLTIP_GAP = 20;

function tooltipLeft(tt: TooltipData, el: HTMLDivElement | null): number {
  const w = el?.offsetWidth ?? 0;
  const showLeft = tt.x > tt.chartWidth / 2;
  if (showLeft) {
    return tt.x - w - TOOLTIP_GAP;
  }
  return tt.x + TOOLTIP_GAP;
}

interface FetchedData {
  labels: string[];
  timestamps: number[];
  series: {
    label: string;
    color: string;
    data: number[];
  }[];
}

/** Returns true if the series labels changed between two datasets. */
function seriesChanged(prev: FetchedData | null, next: FetchedData): boolean {
  if (!prev || prev.series.length !== next.series.length) {
    return true;
  }
  return prev.series.some((s, i) => s.label !== next.series[i].label);
}

export default function TimeSeriesChart({
  title,
  query,
  range,
  unit,
  refreshKey,
  thresholds,
  yMin,
  color: colorOverride,
  from,
  to,
  onRangeSelect,
  onSeriesDoubleClick,
  onSeriesInfo,
  stackable,
  isolatedLabel,
  onIsolationChange,
}: Props) {
  const isMobile = useMatchesBreakpoint("md", "below");
  const chartRef = useRef<ChartJS<"line"> | null>(null);
  const tooltipElRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<State>("loading");
  const [errorMessage, setErrorMessage] = useState("");
  const [tooltip, setTooltip] = useState<TooltipData | null>(null);
  const [fetchedData, setFetchedData] = useState<FetchedData | null>(null);
  const tooltipRef = useRef(setTooltip);
  tooltipRef.current = setTooltip;
  const unitRef = useRef(unit);
  unitRef.current = unit;
  const thresholdsRef = useRef(thresholds);
  thresholdsRef.current = thresholds;
  const fetchedDataRef = useRef(fetchedData);
  fetchedDataRef.current = fetchedData;
  const onSeriesDoubleClickRef = useRef(onSeriesDoubleClick);
  onSeriesDoubleClickRef.current = onSeriesDoubleClick;
  const onRangeSelectRef = useRef(onRangeSelect);
  onRangeSelectRef.current = onRangeSelect;
  const onSeriesInfoRef = useRef(onSeriesInfo);
  onSeriesInfoRef.current = onSeriesInfo;

  const panel = useMetricsPanelContext();
  const [localStacked, setLocalStacked] = useState(false);
  const stacked = panel?.stacked ?? localStacked;
  const stackedRef = useRef(false);
  stackedRef.current = stacked;

  const controlled = isolatedLabel !== undefined;
  const [localIsolatedIndex, setLocalIsolatedIndex] = useState<number | null>(null);
  const controlledIndex = useMemo(() => {
    if (!controlled || isolatedLabel == null || !fetchedData) {
      return null;
    }
    const idx = fetchedData.series.findIndex(({ label }) => label === isolatedLabel);
    return idx >= 0 ? idx : null;
  }, [controlled, isolatedLabel, fetchedData]);
  const isolatedIndex = controlled ? controlledIndex : localIsolatedIndex;
  const setIsolatedIndex = useCallback(
    (index: number | null) => {
      if (controlled) {
        const label = index != null ? (fetchedDataRef.current?.series[index]?.label ?? null) : null;

        onIsolationChange?.(label);
      } else {
        setLocalIsolatedIndex(index);
      }
    },
    [controlled, onIsolationChange],
  );
  const isolatedIndexRef = useRef<number | null>(null);
  isolatedIndexRef.current = isolatedIndex;
  const justZoomedRef = useRef(false);
  const clickTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const chartId = useMemo(() => `tsc-${Math.random().toString(36).slice(2, 8)}`, []);
  const sync = useChartSync();
  const syncTimestampRef = useRef<number | null>(null);
  const syncIndexRef = useRef<number | null>(null);

  useEffect(() => {
    return sync.subscribeIsolation(chartId, (seriesLabel) => {
      const data = fetchedDataRef.current;

      if (!data || seriesLabel == null) {
        setIsolatedIndex(null);

        return;
      }

      const index = data.series.findIndex(({ label }) => label === seriesLabel);

      setIsolatedIndex(index >= 0 ? index : null);
    });
  }, [chartId, sync, setIsolatedIndex]);

  useEffect(() => {
    return sync.subscribe(chartId, (timestamp) => {
      syncTimestampRef.current = timestamp > 0 ? timestamp : null;
      const data = fetchedDataRef.current;

      if (timestamp > 0 && data) {
        syncIndexRef.current = data.timestamps.findIndex((t) => t >= timestamp);
      } else {
        syncIndexRef.current = null;
      }

      chartRef.current?.draw();
    });
  }, [chartId, sync]);

  const fetchData = useCallback(() => {
    setState("loading");

    const rangeSec = rangeIntervals[range] || 3600;
    const now = Math.floor(Date.now() / 1000);
    const start = from ?? now - rangeSec;
    const end = to ?? now;
    const step = Math.max(Math.floor((end - start) / 300), 15);

    let cancelled = false;

    api
      .metricsQueryRange(query, String(start), String(end), String(step))
      .then((resp) => {
        if (cancelled) {
          return;
        }

        const parsed = parseRangeResult(resp, title, colorOverride);

        if (!parsed) {
          if (import.meta.env.DEV) {
            const mock = generateMockSeries(
              title,
              unitRef.current,
              start,
              end,
              step,
              colorOverride,
            );

            setFetchedData(mock);
            onSeriesInfoRef.current?.(mock.series.map((s) => ({ label: s.label, color: s.color })));

            if (seriesChanged(fetchedDataRef.current, mock)) {
              setIsolatedIndex(null);
            }

            setState("data");

            return;
          }

          setState("empty");

          return;
        }

        setFetchedData(parsed);
        onSeriesInfoRef.current?.(parsed.series.map((s) => ({ label: s.label, color: s.color })));

        if (seriesChanged(fetchedDataRef.current, parsed)) {
          setIsolatedIndex(null);
        }

        setState("data");
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
  }, [query, range, from, to, title, colorOverride, setIsolatedIndex]);

  useEffect(() => {
    const cancel = fetchData();

    return () => {
      cancel?.();
    };
  }, [fetchData, refreshKey]);

  // SSE streaming for live ranges. Opens after the initial fetch completes.
  const streaming = panel?.streaming ?? true;
  const hasOpenedRef = useRef(false);
  const [sseKey, setSSEKey] = useState(0);
  const fetchDataRef = useRef(fetchData);
  fetchDataRef.current = fetchData;

  // Reset the gate when query/range changes so SSE re-opens after next fetch
  useEffect(() => {
    hasOpenedRef.current = false;
  }, [query, range, from, to]);

  // Mark ready once we have data
  useEffect(() => {
    if (fetchedData != null) {
      hasOpenedRef.current = true;
    }
  }, [fetchedData]);

  useEffect(() => {
    if (!hasOpenedRef.current || from != null || to != null || !streaming) {
      return;
    }

    const rangeSec = rangeIntervals[range] || 3600;
    const step = Math.max(Math.floor(rangeSec / 300), 15);
    const url = api.metricsStreamURL(query, step, rangeSec);
    const eventSource = new EventSource(url);

    eventSource.addEventListener("initial", (e: MessageEvent) => {
      try {
        const resp = JSON.parse(e.data) as PrometheusResponse;
        const parsed = parseRangeResult(resp, title, colorOverride);

        if (!parsed) {
          return;
        }

        setFetchedData(parsed);
        onSeriesInfoRef.current?.(parsed.series.map(({ color, label }) => ({ color, label })));
        setState("data");
      } catch {
        /* ignore parse errors */
      }
    });

    eventSource.addEventListener("point", (event: MessageEvent) => {
      try {
        const response = JSON.parse(event.data) as PrometheusResponse;

        if (!response.data?.result?.length) {
          return;
        }

        setFetchedData((previous) => {
          if (!previous) {
            return previous;
          }

          const timestamp = Number(response.data.result[0].value![0]);
          const timeLabel = new Date(timestamp * 1000).toLocaleTimeString();
          const newTimestamps = [...previous.timestamps.slice(1), timestamp];
          const newLabels = [...previous.labels.slice(1), timeLabel];
          const newSeries = previous.series.map((series) => {
            const match = response.data.result.find(
              ({ metric }) => seriesLabel(metric) === series.label,
            );
            const value = match ? Number(match.value![1]) : 0;

            return { ...series, data: [...series.data.slice(1), value] };
          });

          return {
            labels: newLabels,
            timestamps: newTimestamps,
            series: newSeries,
          };
        });
      } catch {
        /* ignore parse errors */
      }
    });

    eventSource.addEventListener("query_error", (event: MessageEvent) => {
      console.warn("[metrics stream] Prometheus error:", event.data); // eslint-disable-line no-console
    });

    eventSource.onerror = () => {
      eventSource.close();
    };

    // Close SSE on tab hide; tab show triggers a re-run via sseKey
    const visHandler = () => {
      if (document.visibilityState === "hidden") {
        eventSource.close();
      } else {
        hasOpenedRef.current = false;
        fetchDataRef.current();
        setSSEKey((key) => key + 1);
      }
    };

    document.addEventListener("visibilitychange", visHandler);

    return () => {
      eventSource.close();
      document.removeEventListener("visibilitychange", visHandler);
    };
  }, [query, range, from, to, streaming, title, colorOverride, sseKey]);

  const chartData = useMemo<ChartData<"line"> | null>(() => {
    if (!fetchedData) {
      return null;
    }

    return {
      labels: fetchedData.labels,
      datasets: fetchedData.series.map(({ color, data, label }, i) => {
        const dimmed = isolatedIndex != null && isolatedIndex !== i;
        const base = {
          label: label,
          pointRadius: 0,
          pointHoverRadius: dimmed ? 0 : 3,
          pointHoverBackgroundColor: color,
          pointHoverBorderWidth: 0,
          tension: 0.3,
        } as const;

        if (stacked) {
          return {
            ...base,
            data: dimmed ? data.map(() => 0) : data,
            borderColor: color,
            borderWidth: 1,
            fill: "stack" as const,
            backgroundColor: color + "66",
          };
        }

        return {
          ...base,
          data,
          borderColor: dimmed ? color + "4D" : color,
          borderWidth: 1.5,
          fill: !dimmed,
          backgroundColor: dimmed
            ? "transparent"
            : ({ chart }: { chart: ChartJS }) => {
                if (!chart.chartArea) {
                  return color + "18";
                }

                return makeGradient(chart.ctx, chart.chartArea, color);
              },
        };
      }),
    };
  }, [fetchedData, isolatedIndex, stacked]);

  const suggestedMax = useMemo<number | undefined>(() => {
    if (!thresholds?.length || !fetchedData) {
      return undefined;
    }

    let high = Math.max(...fetchedData.series.flatMap(({ data }) => data));

    for (const threshold of thresholds) {
      high = Math.max(high, threshold.value);
    }

    const low = yMin ?? Math.min(...fetchedData.series.flatMap(({ data }) => data));

    return high + (high - low) * 0.1 || high + 1;
  }, [thresholds, fetchedData, yMin]);

  const thresholdPlugin = useMemo<Plugin<"line">>(
    () => ({
      id: "thresholdLines",
      afterDatasetsDraw(chart) {
        const thresholds = thresholdsRef.current;

        if (!thresholds?.length) {
          return;
        }

        const { ctx, chartArea, scales } = chart;
        const yScale = scales.y;

        if (!yScale || !chartArea) {
          return;
        }

        for (const threshold of thresholds) {
          const yPosition = yScale.getPixelForValue(threshold.value);

          if (yPosition < chartArea.top || yPosition > chartArea.top + chartArea.height) {
            continue;
          }

          ctx.save();
          ctx.strokeStyle = threshold.color;
          ctx.lineWidth = 1.5;

          if (threshold.dash) {
            ctx.setLineDash(threshold.dash);
          }

          ctx.beginPath();
          ctx.moveTo(chartArea.left, yPosition);
          ctx.lineTo(chartArea.right, yPosition);
          ctx.stroke();
          ctx.restore();
        }
      },
    }),
    [],
  );

  const crosshairPlugin = useMemo<Plugin<"line">>(
    () => ({
      id: "crosshair",
      afterEvent(chart, { event: { native, type, x, x: cx, y: cy } }) {
        if (type === "mouseout") {
          tooltipRef.current(null);
          sync.publish(chartId, -1);
          chart.draw();

          return;
        }

        if (type === "dblclick") {
          if (clickTimerRef.current) {
            clearTimeout(clickTimerRef.current);

            clickTimerRef.current = null;
          }

          const elements = chart.getElementsAtEventForMode(
            native as Event,
            "nearest",
            { intersect: false },
            false,
          );

          if (elements.length > 0 && onSeriesDoubleClickRef.current) {
            const label = chart.data.datasets[elements[0].datasetIndex]?.label;

            if (label) {
              onSeriesDoubleClickRef.current(label);
            }
          }

          return;
        }

        if (type === "click") {
          if (justZoomedRef.current) {
            justZoomedRef.current = false;

            return;
          }

          if (cx == null || cy == null) {
            return;
          }
          const elements = chart.getElementsAtEventForMode(
            native as Event,
            "nearest",
            { intersect: false },
            false,
          );

          const datasets = chart.data.datasets;

          if (clickTimerRef.current) {
            clearTimeout(clickTimerRef.current);
          }

          clickTimerRef.current = setTimeout(() => {
            clickTimerRef.current = null;

            if (elements.length > 0) {
              const clickedIdx = elements[0].datasetIndex;
              const wasIsolated = isolatedIndexRef.current === clickedIdx;
              const newIndex = wasIsolated ? null : clickedIdx;

              setIsolatedIndex(newIndex);

              const label = newIndex != null ? (datasets[newIndex]?.label ?? null) : null;

              sync.publishIsolation(chartId, label);
            } else {
              setIsolatedIndex(null);
              sync.publishIsolation(chartId, null);
            }
          }, 250);

          return;
        }

        if (type !== "mousemove") {
          return;
        }

        if (x == null) {
          return;
        }

        const { chartArea, scales } = chart;

        if (!chartArea || !scales.x) {
          return;
        }

        if (x < chartArea.left || x > chartArea.right) {
          tooltipRef.current(null);

          return;
        }

        const xScale = scales.x;
        const xValue = xScale.getValueForPixel(x);

        if (xValue == null) {
          return;
        }

        const index = Math.round(xValue);
        const datasets = chart.data.datasets;

        if (index < 0 || !datasets.length || index >= (datasets[0].data?.length ?? 0)) {
          return;
        }

        const items: TooltipData["series"] = [];

        for (const dataset of datasets) {
          const value = dataset.data[index] as number;

          if (value == null) {
            continue;
          }

          items.push({
            label: dataset.label ?? "value",
            color: dataset.borderColor as string,
            value: formatValue(value, unitRef.current),
            raw: value,
          });
        }
        const thresholds = thresholdsRef.current;

        if (thresholds?.length) {
          for (const { color, label, value } of thresholds) {
            items.push({
              label: label,
              color: color,
              value: formatValue(value, unitRef.current),
              raw: value,
              dashed: true,
            });
          }
        }

        items.sort((a, b) => b.raw - a.raw);

        if (stackedRef.current && fetchedDataRef.current?.series) {
          const total = fetchedDataRef.current.series.reduce((sum, { data }) => {
            const value = data[index] ?? 0;

            return sum + value;
          }, 0);

          items.unshift({
            label: "Total",
            color: "transparent",
            value: formatValue(total, unitRef.current),
            raw: total,
          });
        }

        const fetched = fetchedDataRef.current;
        const timestamp = fetched?.timestamps[index];
        const time = timestamp ? new Date(timestamp * 1000).toLocaleTimeString() : "";

        tooltipRef.current({
          time,
          series: items,
          x,
          chartWidth: chartArea.right,
          top: chartArea.top + 8,
        });

        if (timestamp != null) {
          sync.publish(chartId, timestamp);
        }
      },
      afterDraw(chart) {
        const { ctx, chartArea } = chart;

        if (!chartArea) {
          return;
        }

        // Draw crosshair line at cursor position
        const active = chart.getActiveElements();

        if (active.length) {
          const x = active[0].element.x;

          ctx.save();
          ctx.beginPath();
          ctx.moveTo(x, chartArea.top);
          ctx.lineTo(x, chartArea.bottom);
          ctx.lineWidth = 1;
          ctx.strokeStyle = getSemanticChartColor("crosshair");
          ctx.stroke();
          ctx.restore();
        }

        // Draw synced crosshair from sibling chart (uses cached index)
        const syncIndex = syncIndexRef.current;

        if (syncIndex != null && syncIndex >= 0) {
          const xPixel = chart.scales.x?.getPixelForValue(syncIndex);

          if (xPixel != null && xPixel >= chartArea.left && xPixel <= chartArea.right) {
            ctx.save();
            ctx.beginPath();
            ctx.moveTo(xPixel, chartArea.top);
            ctx.lineTo(xPixel, chartArea.bottom);

            ctx.lineWidth = 1;
            ctx.strokeStyle = getSemanticChartColor("crosshair");

            ctx.setLineDash([4, 4]);
            ctx.stroke();

            const datasets = chart.data.datasets;

            for (let seriesIndex = 0; seriesIndex < datasets.length; seriesIndex++) {
              const value = datasets[seriesIndex].data[syncIndex] as number;

              if (value == null) {
                continue;
              }

              const yPixel = chart.scales.y.getPixelForValue(value);

              ctx.beginPath();
              ctx.arc(xPixel, yPixel, 3, 0, Math.PI * 2);

              ctx.fillStyle = datasets[seriesIndex].borderColor as string;

              ctx.fill();
            }

            ctx.restore();
          }
        }
      },
    }),
    [chartId, sync, setIsolatedIndex],
  );

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      // dblclick is not in Chart.js's type union but is dispatched by the browser canvas
      events: [
        "mousemove",
        "mouseout",
        "click",
        "dblclick",
        "touchstart",
        "touchmove",
      ] as unknown as ChartOptions<"line">["events"],
      interaction: {
        mode: "index",
        intersect: false,
      },
      layout: { padding: 0 },
      plugins: {
        legend: { display: false },
        tooltip: { enabled: false },
        zoom: {
          zoom: {
            drag: {
              enabled: !isMobile,
              backgroundColor: getSemanticChartColor("zoom"),
              borderColor: getSemanticChartColor("zoom"),
              borderWidth: 1,
              threshold: 5,
            },
            mode: "x" as const,
            onZoom: ({ chart }: { chart: ChartJS }) => {
              justZoomedRef.current = true;
              const data = fetchedDataRef.current;
              const callback = onRangeSelectRef.current;

              if (!callback || !data) {
                return;
              }

              const xScale = chart.scales.x;
              const minIndex = Math.max(0, Math.floor(xScale.min));
              const maxIndex = Math.min(data.timestamps.length - 1, Math.ceil(xScale.max));
              const fromTimestamp = data.timestamps[minIndex];
              const toTimestamp = data.timestamps[maxIndex];

              if (fromTimestamp && toTimestamp) {
                callback(fromTimestamp, toTimestamp);
              }

              chart.resetZoom();
            },
          },
        },
      },
      scales: {
        x: {
          display: false,
        },
        y: {
          display: false,
          stacked: stacked || undefined,
          min: yMin,
          suggestedMax,
        },
      },
      elements: {
        point: { radius: 0 },
      },
    }),
    [yMin, suggestedMax, stacked, isMobile],
  );

  const plugins = useMemo(
    () => [thresholdPlugin, crosshairPlugin],
    [thresholdPlugin, crosshairPlugin],
  );

  return (
    <div className="overflow-visible rounded-lg border bg-card">
      <div className="flex items-center gap-2 px-4 pt-4 pb-2">
        <span className="text-sm font-medium">{title}</span>

        {stackable && panel?.stacked == null && (
          <div className="ml-1 flex items-center gap-0.5">
            <button
              onClick={() => setLocalStacked(false)}
              aria-pressed={!stacked}
              className="rounded p-0.5 hover:bg-muted/50 aria-pressed:bg-muted"
              title="Line chart"
            >
              <LineChart className="size-3.5" />
            </button>
            <button
              onClick={() => setLocalStacked(true)}
              aria-pressed={stacked}
              className="rounded p-0.5 hover:bg-muted/50 aria-pressed:bg-muted"
              title="Stacked area"
            >
              <AreaChart className="size-3.5" />
            </button>
          </div>
        )}

        {unit && <span className="ml-auto text-xs text-muted-foreground">{unit}</span>}
      </div>

      {state === "loading" && !fetchedData && <div className="h-50 rounded bg-muted/50" />}

      {state === "error" && (
        <div className="flex h-50 items-center justify-center rounded border border-destructive/20 bg-destructive/5">
          <div className="text-center">
            <p className="mb-2 text-sm text-destructive">{errorMessage}</p>
            <button
              onClick={fetchData}
              className="inline-flex items-center gap-1.5 rounded-md border border-destructive/30 px-3 py-1.5 text-xs text-destructive hover:bg-destructive/10"
            >
              <RefreshCw className="size-3" />
              Retry
            </button>
          </div>
        </div>
      )}

      {state === "empty" && (
        <div className="flex h-50 items-center justify-center rounded bg-muted/30">
          <div className="text-center text-muted-foreground">
            <BarChart3 className="mx-auto mb-2 size-8 opacity-30" />
            <p className="text-sm">No data for this time range</p>
          </div>
        </div>
      )}

      <div className="relative">
        {state === "loading" && fetchedData && (
          <div className="absolute top-2 right-2 z-10">
            <RefreshCw className="size-3.5 animate-spin text-muted-foreground" />
          </div>
        )}
        <div
          className="overflow-hidden rounded-b-lg"
          hidden={state !== "data"}
        >
          {chartData && (
            <div className="h-50">
              <Line
                ref={chartRef}
                data={chartData}
                options={options}
                plugins={plugins}
              />
            </div>
          )}
        </div>
        <div
          ref={tooltipElRef}
          className={chartTooltipClasses}
          style={{
            left: tooltip ? tooltipLeft(tooltip, tooltipElRef.current) : 0,
            top: tooltip?.top ?? 0,
            opacity: tooltip && state === "data" ? 1 : 0,
            transition: "opacity 100ms ease",
          }}
        >
          {tooltip && (
            <>
              <div className="mb-1.5 font-semibold text-foreground">{tooltip.time}</div>
              {tooltip.series.map(({ color, dashed, label, value }) => (
                <div
                  key={label}
                  className="flex items-center gap-2 whitespace-nowrap"
                >
                  {dashed ? (
                    <span
                      className="w-3 shrink-0 border-t-2 border-dashed"
                      style={{ borderColor: color }}
                    />
                  ) : (
                    <span
                      className="h-3 w-1 shrink-0 rounded-sm"
                      style={{ background: color }}
                    />
                  )}

                  <span className="text-muted-foreground">{label}</span>
                  <span className="ms-auto ps-4 font-semibold text-foreground">{value}</span>
                </div>
              ))}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
