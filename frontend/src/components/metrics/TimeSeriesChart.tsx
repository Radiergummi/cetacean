import { useEffect, useRef, useState, useCallback, useMemo } from "react";
import { RefreshCw, BarChart3 } from "lucide-react";
import {
  Chart as ChartJS,
  LineElement,
  PointElement,
  LinearScale,
  CategoryScale,
  Filler,
  Tooltip as ChartTooltip,
  type ChartData,
  type ChartOptions,
  type Plugin,
} from "chart.js";
import zoomPlugin from "chartjs-plugin-zoom";
import { Line } from "react-chartjs-2";
import { api } from "../../api/client";
import { getChartColor } from "../../lib/chartColors";
import { useChartSync } from "./ChartSyncProvider";

ChartJS.register(LineElement, PointElement, LinearScale, CategoryScale, Filler, ChartTooltip, zoomPlugin);

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
  syncKey?: string;
  /** Force y-axis minimum value (e.g. 0 to always start at zero). */
  yMin?: number;
  /** Override the default series color. */
  color?: string;
  from?: number;
  to?: number;
  onRangeSelect?: (from: number, to: number) => void;
  onSeriesDoubleClick?: (seriesLabel: string) => void;
  onSeriesInfo?: (series: { label: string; color: string }[]) => void;
}

type State = "loading" | "data" | "empty" | "error";

interface TooltipData {
  time: string;
  series: { label: string; color: string; value: string; raw: number; dashed?: boolean }[];
  x: number;
  chartWidth: number;
  top: number;
}

const RANGE_SECONDS: Record<string, number> = {
  "1h": 3600,
  "6h": 21600,
  "24h": 86400,
  "7d": 604800,
};


function formatValue(v: number, unit?: string): string {
  if (unit === "bytes" || unit === "bytes/s") {
    if (v >= 1e9) return `${(v / 1e9).toFixed(1)} GB${unit === "bytes/s" ? "/s" : ""}`;
    if (v >= 1e6) return `${(v / 1e6).toFixed(1)} MB${unit === "bytes/s" ? "/s" : ""}`;
    if (v >= 1e3) return `${(v / 1e3).toFixed(1)} KB${unit === "bytes/s" ? "/s" : ""}`;
    return `${v.toFixed(0)} B${unit === "bytes/s" ? "/s" : ""}`;
  }
  if (unit === "%") return `${v.toFixed(1)}%`;
  if (unit === "cores") return `${v.toFixed(3)}`;
  return v.toFixed(2);
}

function seriesLabel(metric: Record<string, string> | undefined, fallback?: string): string {
  if (!metric) return fallback ?? "value";
  const { __name__, ...labels } = metric;
  const labelStr = Object.values(labels).filter(Boolean).join(", ");
  if (labelStr) return labelStr;
  if (__name__) return __name__;
  return fallback ?? "value";
}

/** Create a vertical gradient fill for a series color. */
function makeGradient(ctx: CanvasRenderingContext2D, chartArea: { top: number; bottom: number }, color: string) {
  const grad = ctx.createLinearGradient(0, chartArea.top, 0, chartArea.bottom);
  grad.addColorStop(0, color + "30");
  grad.addColorStop(1, color + "00");
  return grad;
}

const TOOLTIP_GAP = 20;

function tooltipLeft(tt: TooltipData, el: HTMLDivElement | null): number {
  const w = el?.offsetWidth ?? 0;
  const showLeft = tt.x > tt.chartWidth / 2;
  if (showLeft) return tt.x - w - TOOLTIP_GAP;
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
}: Props) {
  const chartRef = useRef<ChartJS<"line"> | null>(null);
  const tooltipElRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<State>("loading");
  const [errorMsg, setErrorMsg] = useState("");
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

  const [isolatedIndex, setIsolatedIndex] = useState<number | null>(null);
  const justZoomedRef = useRef(false);

  const chartId = useMemo(() => `tsc-${Math.random().toString(36).slice(2, 8)}`, []);
  const sync = useChartSync();
  const syncTimestampRef = useRef<number | null>(null);
  const syncIndexRef = useRef<number | null>(null);

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

    const rangeSec = RANGE_SECONDS[range] || 3600;
    const now = Math.floor(Date.now() / 1000);
    const start = from ?? now - rangeSec;
    const end = to ?? now;
    const step = Math.max(Math.floor((end - start) / 300), 15);

    let cancelled = false;

    api
      .metricsQueryRange(query, String(start), String(end), String(step))
      .then((resp) => {
        if (cancelled) return;

        if (!resp.data?.result?.length) {
          setState("empty");
          return;
        }

        const result = resp.data.result;
        const timestamps = result[0].values!.map((v) => Number(v[0]));
        const labels = timestamps.map((ts) => new Date(ts * 1000).toLocaleTimeString());

        const series = result.map((s, i) => ({
          label: seriesLabel(s.metric, result.length === 1 ? title : undefined),
          color: colorOverride ?? getChartColor(i),
          data: s.values!.map((v) => Number(v[1])),
        }));

        setFetchedData({ labels, timestamps, series });
        onSeriesInfo?.(series.map((s) => ({ label: s.label, color: s.color })));
        setIsolatedIndex(null);
        setState("data");
      })
      .catch((err) => {
        if (!cancelled) {
          setErrorMsg(err instanceof Error ? err.message : "Failed to load metrics");
          setState("error");
        }
      });

    return () => {
      cancelled = true;
    };
  }, [query, range, from, to, title, colorOverride]);

  useEffect(() => {
    const cancel = fetchData();
    return () => {
      cancel?.();
    };
  }, [fetchData, refreshKey]);

  const chartData: ChartData<"line"> | null = fetchedData
    ? {
        labels: fetchedData.labels,
        datasets: fetchedData.series.map((s, i) => {
          const dimmed = isolatedIndex != null && isolatedIndex !== i;
          return {
            label: s.label,
            data: s.data,
            borderColor: dimmed ? s.color + "4D" : s.color,
            borderWidth: 1.5,
            pointRadius: 0,
            pointHoverRadius: dimmed ? 0 : 3,
            pointHoverBackgroundColor: s.color,
            pointHoverBorderWidth: 0,
            tension: 0.3,
            fill: !dimmed,
            backgroundColor: dimmed
              ? "transparent"
              : (ctx: { chart: ChartJS }) => {
                  const chart = ctx.chart;
                  if (!chart.chartArea) return s.color + "18";
                  return makeGradient(chart.ctx, chart.chartArea, s.color);
                },
          };
        }),
      }
    : null;

  // Compute y-axis bounds
  let suggestedMax: number | undefined;
  if (thresholds?.length && fetchedData) {
    const dataMax = Math.max(...fetchedData.series.flatMap((s) => s.data));
    let hi = dataMax;
    for (const t of thresholds) hi = Math.max(hi, t.value);
    const lo = yMin ?? Math.min(...fetchedData.series.flatMap((s) => s.data));
    suggestedMax = hi + (hi - lo) * 0.1 || hi + 1;
  }

  const thresholdPlugin = useMemo<Plugin<"line">>(() => ({
    id: "thresholdLines",
    afterDatasetsDraw(chart) {
      const ts = thresholdsRef.current;
      if (!ts?.length) return;
      const { ctx, chartArea, scales } = chart;
      const yScale = scales.y;
      if (!yScale || !chartArea) return;

      for (const t of ts) {
        const yPos = yScale.getPixelForValue(t.value);
        if (yPos < chartArea.top || yPos > chartArea.top + chartArea.height) continue;
        ctx.save();
        ctx.strokeStyle = t.color;
        ctx.lineWidth = 1.5;
        if (t.dash) ctx.setLineDash(t.dash);
        ctx.beginPath();
        ctx.moveTo(chartArea.left, yPos);
        ctx.lineTo(chartArea.right, yPos);
        ctx.stroke();
        ctx.restore();
      }
    },
  }), []);

  const crosshairPlugin = useMemo<Plugin<"line">>(() => ({
    id: "crosshair",
    afterEvent(chart, args) {
      if (args.event.type === "mouseout") {
        tooltipRef.current(null);
        sync.publish(chartId, -1);
        chart.draw();
        return;
      }
      if (args.event.type === "dblclick") {
        const elements = chart.getElementsAtEventForMode(
          args.event.native as Event,
          "nearest",
          { intersect: false, axis: "x" },
          false,
        );
        if (elements.length > 0 && onSeriesDoubleClickRef.current) {
          const label = chart.data.datasets[elements[0].datasetIndex]?.label;
          if (label) onSeriesDoubleClickRef.current(label);
        }
        return;
      }
      if (args.event.type === "click") {
        if (justZoomedRef.current) {
          justZoomedRef.current = false;
          return;
        }
        const { x: cx, y: cy } = args.event;
        if (cx == null || cy == null) return;
        const elements = chart.getElementsAtEventForMode(
          args.event.native as Event,
          "nearest",
          { intersect: false, axis: "x" },
          false,
        );
        if (elements.length > 0) {
          const clickedIdx = elements[0].datasetIndex;
          setIsolatedIndex((prev) => (prev === clickedIdx ? null : clickedIdx));
        } else {
          setIsolatedIndex(null);
        }
        return;
      }
      if (args.event.type !== "mousemove") return;

      const { x } = args.event;
      if (x == null) return;
      const { chartArea, scales } = chart;
      if (!chartArea || !scales.x) return;
      if (x < chartArea.left || x > chartArea.right) {
        tooltipRef.current(null);
        return;
      }

      const xScale = scales.x;
      const xVal = xScale.getValueForPixel(x);
      if (xVal == null) return;
      const idx = Math.round(xVal);
      const ds = chart.data.datasets;
      if (idx < 0 || !ds.length || idx >= (ds[0].data?.length ?? 0)) return;

      const items: TooltipData["series"] = [];
      for (const dataset of ds) {
        const v = dataset.data[idx] as number;
        if (v == null) continue;
        items.push({
          label: dataset.label ?? "value",
          color: dataset.borderColor as string,
          value: formatValue(v, unitRef.current),
          raw: v,
        });
      }
      const ts = thresholdsRef.current;
      if (ts?.length) {
        for (const t of ts) {
          items.push({ label: t.label, color: t.color, value: formatValue(t.value, unitRef.current), raw: t.value, dashed: true });
        }
      }
      items.sort((a, b) => b.raw - a.raw);

      const fetched = fetchedDataRef.current;
      const timestamp = fetched?.timestamps[idx];
      const time = timestamp ? new Date(timestamp * 1000).toLocaleTimeString() : "";

      tooltipRef.current({
        time,
        series: items,
        x: x,
        chartWidth: chartArea.right,
        top: chartArea.top + 8,
      });

      if (timestamp != null) sync.publish(chartId, timestamp);
    },
    afterDraw(chart) {
      const { ctx, chartArea } = chart;
      if (!chartArea) return;
      // Draw crosshair line at cursor position
      const active = chart.getActiveElements();
      if (active.length) {
        const x = active[0].element.x;
        ctx.save();
        ctx.beginPath();
        ctx.moveTo(x, chartArea.top);
        ctx.lineTo(x, chartArea.bottom);
        ctx.lineWidth = 1;
        ctx.strokeStyle = "rgba(136,136,136,0.3)";
        ctx.stroke();
        ctx.restore();
      }

      // Draw synced crosshair from sibling chart (uses cached index)
      const syncIdx = syncIndexRef.current;
      if (syncIdx != null && syncIdx >= 0) {
        const xPixel = chart.scales.x?.getPixelForValue(syncIdx);
        if (xPixel != null && xPixel >= chartArea.left && xPixel <= chartArea.right) {
          ctx.save();
          ctx.beginPath();
          ctx.moveTo(xPixel, chartArea.top);
          ctx.lineTo(xPixel, chartArea.bottom);
          ctx.lineWidth = 1;
          ctx.strokeStyle = "rgba(136,136,136,0.2)";
          ctx.setLineDash([4, 4]);
          ctx.stroke();

          const datasets = chart.data.datasets;
          for (let si = 0; si < datasets.length; si++) {
            const val = datasets[si].data[syncIdx] as number;
            if (val == null) continue;
            const yPixel = chart.scales.y.getPixelForValue(val);
            ctx.beginPath();
            ctx.arc(xPixel, yPixel, 3, 0, Math.PI * 2);
            ctx.fillStyle = datasets[si].borderColor as string;
            ctx.fill();
          }
          ctx.restore();
        }
      }
    },
  }), [chartId, sync]);

  const options = useMemo<ChartOptions<"line">>(() => ({
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    // dblclick is not in Chart.js's type union but is dispatched by the browser canvas
    events: ['mousemove', 'mouseout', 'click', 'dblclick', 'touchstart', 'touchmove'] as unknown as ChartOptions<"line">["events"],
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
            enabled: true,
            backgroundColor: "rgba(100, 143, 255, 0.1)",
            borderColor: "rgba(100, 143, 255, 0.3)",
            borderWidth: 1,
            threshold: 5,
          },
          mode: "x" as const,
          onZoom: ({ chart }: { chart: ChartJS }) => {
            justZoomedRef.current = true;
            const data = fetchedDataRef.current;
            const cb = onRangeSelectRef.current;
            if (!cb || !data) return;
            const xScale = chart.scales.x;
            const minIdx = Math.max(0, Math.floor(xScale.min));
            const maxIdx = Math.min(data.timestamps.length - 1, Math.ceil(xScale.max));
            const fromTs = data.timestamps[minIdx];
            const toTs = data.timestamps[maxIdx];
            if (fromTs && toTs) cb(fromTs, toTs);
            chart.resetZoom();
          },
        },
      },
    },
    scales: {
      x: { display: false },
      y: {
        display: false,
        min: yMin,
        suggestedMax,
      },
    },
    elements: {
      point: { radius: 0 },
    },
  }), [yMin, suggestedMax]);

  const plugins = useMemo(() => [thresholdPlugin, crosshairPlugin], [thresholdPlugin, crosshairPlugin]);

  return (
    <div className="rounded-lg border bg-card overflow-visible">
      <div className="flex items-center justify-between px-4 pt-4 pb-2">
        <span className="text-sm font-medium">{title}</span>
        {unit && <span className="text-xs text-muted-foreground">{unit}</span>}
      </div>

      {state === "loading" && !fetchedData && <div className="h-[200px] rounded bg-muted/50" />}

      {state === "error" && (
        <div className="h-[200px] rounded bg-destructive/5 border border-destructive/20 flex items-center justify-center">
          <div className="text-center">
            <p className="text-sm text-destructive mb-2">{errorMsg}</p>
            <button
              onClick={fetchData}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md border border-destructive/30 text-destructive hover:bg-destructive/10"
            >
              <RefreshCw className="size-3" />
              Retry
            </button>
          </div>
        </div>
      )}

      {state === "empty" && (
        <div className="h-[200px] rounded bg-muted/30 flex items-center justify-center">
          <div className="text-center text-muted-foreground">
            <BarChart3 className="size-8 mx-auto mb-2 opacity-30" />
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
        <div className="overflow-hidden rounded-b-lg" hidden={state !== "data"}>
          {chartData && (
            <div className="h-[200px]">
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
          className="absolute pointer-events-none z-20 rounded-md ring-1 ring-border/50 bg-popover/80 backdrop-blur-sm backdrop-saturate-200 px-3 py-2.5 text-xs leading-snug shadow-lg"
          style={{
            left: tooltip ? tooltipLeft(tooltip, tooltipElRef.current) : 0,
            top: tooltip?.top ?? 0,
            opacity: tooltip && state === "data" ? 1 : 0,
            transition: tooltip ? "opacity 50ms ease" : "opacity 100ms ease",
          }}
        >
          {tooltip && (
            <>
              <div className="font-semibold mb-1.5 text-foreground">{tooltip.time}</div>
              {tooltip.series.map((s) => (
                <div key={s.label} className="flex items-center gap-2 whitespace-nowrap">
                  {s.dashed ? (
                    <span className="w-3 shrink-0 border-t-2 border-dashed" style={{ borderColor: s.color }} />
                  ) : (
                    <span className="w-1 shrink-0 h-3 rounded-sm" style={{ background: s.color }} />
                  )}
                  <span className="text-muted-foreground">{s.label}</span>
                  <span className="font-semibold ms-auto ps-4 text-foreground">{s.value}</span>
                </div>
              ))}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
