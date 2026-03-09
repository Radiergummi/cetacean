import { useEffect, useRef, useState, useCallback } from "react";
import { RefreshCw, BarChart3 } from "lucide-react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { api } from "../api/client";

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
}

type State = "loading" | "data" | "empty" | "error";

interface TooltipData {
  time: string;
  series: { label: string; color: string; value: string; dashed?: boolean }[];
  /** Cursor x relative to the plot area. */
  cursorX: number;
  /** Plot area left offset in CSS pixels. */
  plotLeft: number;
  /** Plot area width in CSS pixels. */
  plotWidth: number;
  top: number;
}

const RANGE_SECONDS: Record<string, number> = {
  "1h": 3600,
  "6h": 21600,
  "24h": 86400,
  "7d": 604800,
};

const CHART_COLORS = [
  "#4f8cf6", // blue
  "#f59e0b", // amber/orange
  "#34d399", // green
  "#ef4444", // red
  "#a78bfa", // purple
  "#22d3ee", // cyan
  "#e879a8", // magenta
  "#facc15", // yellow
];

/** Create a vertical gradient fill for a series color. */
function gradientFill(color: string): uPlot.Series.Fill {
  return (u: uPlot, seriesIdx: number) => {
    const s = u.series[seriesIdx];
    const yScale = u.scales[s.scale!];
    if (yScale.min == null || yScale.max == null) return color + "18";
    const y0 = u.valToPos(yScale.min, s.scale!, true);
    const y1 = u.valToPos(yScale.max, s.scale!, true);
    const grad = u.ctx.createLinearGradient(0, y1, 0, y0);
    grad.addColorStop(0, color + "00");
    grad.addColorStop(1, color + "30");
    return grad;
  };
}

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

const TOOLTIP_GAP = 20;

function tooltipLeft(tt: TooltipData, el: HTMLDivElement | null): number {
  const w = el?.offsetWidth ?? 0;
  const showLeft = tt.cursorX > tt.plotWidth / 2;
  if (showLeft) return tt.plotLeft + tt.cursorX - w - TOOLTIP_GAP;
  return tt.plotLeft + tt.cursorX + TOOLTIP_GAP;
}

export default function TimeSeriesChart({
  title,
  query,
  range,
  unit,
  refreshKey,
  thresholds,
  syncKey,
  yMin,
  color: colorOverride,
}: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);
  const tooltipElRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<State>("loading");
  const [errorMsg, setErrorMsg] = useState("");
  const [tooltip, setTooltip] = useState<TooltipData | null>(null);
  const tooltipRef = useRef(setTooltip);
  tooltipRef.current = setTooltip;

  const fetchData = useCallback(() => {
    setState("loading");

    const rangeSec = RANGE_SECONDS[range] || 3600;
    const now = Math.floor(Date.now() / 1000);
    const start = now - rangeSec;
    const step = Math.max(Math.floor(rangeSec / 300), 15);

    let cancelled = false;

    api
      .metricsQueryRange(query, String(start), String(now), String(step))
      .then((resp: any) => {
        if (cancelled) return;

        if (!resp.data?.result?.length) {
          setState("empty");
          return;
        }

        const series = resp.data.result;
        const timestamps = series[0].values.map((v: any) => Number(v[0]));
        const data: uPlot.AlignedData = [
          timestamps,
          ...series.map((s: any) => s.values.map((v: any) => Number(v[1]))),
        ];

        if (chartRef.current) chartRef.current.destroy();
        if (!containerRef.current) return;

        const thresholdPlugin: uPlot.Plugin = {
          hooks: {
            draw: [
              (u: uPlot) => {
                if (!thresholds?.length) return;
                const ctx = u.ctx;
                const yAxis = u.scales.y;
                if (yAxis.min == null || yAxis.max == null) return;
                const plotLeft = u.bbox.left;
                const plotWidth = u.bbox.width;

                for (const t of thresholds) {
                  const yPos = u.valToPos(t.value, "y", true);
                  if (yPos < u.bbox.top || yPos > u.bbox.top + u.bbox.height) continue;

                  ctx.save();
                  ctx.strokeStyle = t.color;
                  ctx.lineWidth = 1.5;
                  if (t.dash) ctx.setLineDash(t.dash);
                  ctx.beginPath();
                  ctx.moveTo(plotLeft, yPos);
                  ctx.lineTo(plotLeft + plotWidth, yPos);
                  ctx.stroke();
                  ctx.restore();
                }
              },
            ],
          },
        };

        const crosshairPlugin: uPlot.Plugin = {
          hooks: {
            setCursor: [
              (u: uPlot) => {
                const { idx } = u.cursor;
                if (idx == null) {
                  tooltipRef.current(null);
                  return;
                }
                const ts = u.data[0][idx];
                const items: TooltipData["series"] = [];
                for (let i = 1; i < u.series.length; i++) {
                  const s = u.series[i];
                  if (!s.show) continue;
                  const v = u.data[i][idx];
                  if (v == null) continue;
                  items.push({
                    label: String(s.label ?? `series ${i}`),
                    color: String(s.stroke ?? "#888"),
                    value: formatValue(v, unit),
                  });
                }
                if (thresholds?.length) {
                  for (const t of thresholds) {
                    items.push({
                      label: t.label,
                      color: t.color,
                      value: formatValue(t.value, unit),
                      dashed: true,
                    });
                  }
                }
                const cursorX = u.valToPos(ts, "x");
                const plotLeft = u.bbox.left / devicePixelRatio;
                const plotWidth = u.bbox.width / devicePixelRatio;
                tooltipRef.current({
                  time: new Date(ts * 1000).toLocaleTimeString(),
                  series: items,
                  cursorX,
                  plotLeft,
                  plotWidth,
                  top: u.bbox.top / devicePixelRatio + 8,
                });
              },
            ],
            drawCursor: [
              (u: uPlot) => {
                const { idx } = u.cursor;
                if (idx == null) return;
                const cx = Math.round(u.valToPos(u.data[0][idx], "x", true));
                const { ctx } = u;
                ctx.save();
                ctx.beginPath();
                ctx.moveTo(cx, u.bbox.top);
                ctx.lineTo(cx, u.bbox.top + u.bbox.height);
                ctx.lineWidth = 1;
                ctx.strokeStyle = "rgba(136,136,136,0.3)";
                ctx.stroke();
                ctx.restore();
              },
            ],
          },
        };

        const opts: uPlot.Options = {
          width: containerRef.current.clientWidth || 600,
          height: 200,
          plugins: [thresholdPlugin, crosshairPlugin],
          legend: { show: false },
          padding: [0, 0, 0, 0],
          cursor: {
            drag: { x: false, y: false },
            sync: syncKey ? { key: syncKey, setSeries: true, scales: ["x", null] } : undefined,
            x: false,
            y: false,
            points: {
              size: 6,
              fill: "currentColor",
              stroke: "transparent",
              width: 0,
            },
          },
          focus: { alpha: 0.3 },
          series: [
            {},
            ...series.map((_s: any, i: number) => {
              const color = colorOverride ?? CHART_COLORS[i % CHART_COLORS.length];
              return {
                label: seriesLabel(_s.metric, series.length === 1 ? title : undefined),
                stroke: color,
                width: 1.5,
                fill: gradientFill(color),
                paths: uPlot.paths.spline!(),
                points: { show: false },
              };
            }),
          ],
          scales: {
            y: {
              range: (_u: uPlot, dataMin: number, dataMax: number) => {
                let lo = yMin != null ? Math.min(yMin, dataMin) : dataMin;
                let hi = dataMax;
                if (thresholds?.length) {
                  for (const t of thresholds) {
                    hi = Math.max(hi, t.value);
                  }
                }
                const pad = (hi - lo) * 0.1 || 1;
                return [lo, hi + pad];
              },
            },
          },
          axes: [
            { show: false },
            {
              show: false,
              grid: { stroke: "rgba(136,136,136,0.08)", width: 1 },
            },
          ],
        };

        chartRef.current = new uPlot(opts, data, containerRef.current);
        setState("data");
      })
      .catch(() => {
        if (!cancelled) {
          setErrorMsg("Failed to load metrics");
          setState("error");
        }
      });

    return () => {
      cancelled = true;
    };
  }, [query, range]);

  useEffect(() => {
    const cancel = fetchData();
    return () => {
      cancel?.();
      if (chartRef.current) {
        chartRef.current.destroy();
        chartRef.current = null;
      }
    };
  }, [fetchData, refreshKey]);

  // Resize observer
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      if (chartRef.current && el.clientWidth > 0) {
        chartRef.current.setSize({ width: el.clientWidth, height: 200 });
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  return (
    <div className="rounded-lg border bg-card overflow-hidden">
      <div className="flex items-center justify-between px-4 pt-4 pb-2">
        <span className="text-sm font-medium">{title}</span>
        {unit && <span className="text-xs text-muted-foreground">{unit}</span>}
      </div>

      {state === "loading" && <div className="h-[200px] rounded bg-muted/50" />}

      {state === "error" && (
        <div className="h-[200px] rounded bg-destructive/5 border border-destructive/20 flex items-center justify-center">
          <div className="text-center">
            <p className="text-sm text-destructive mb-2">{errorMsg}</p>
            <button
              onClick={fetchData}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-md border border-destructive/30 text-destructive hover:bg-destructive/10"
            >
              <RefreshCw className="w-3 h-3" />
              Retry
            </button>
          </div>
        </div>
      )}

      {state === "empty" && (
        <div className="h-[200px] rounded bg-muted/30 flex items-center justify-center">
          <div className="text-center text-muted-foreground">
            <BarChart3 className="w-8 h-8 mx-auto mb-2 opacity-30" />
            <p className="text-sm">No data for this time range</p>
          </div>
        </div>
      )}

      <div className="relative">
        <div ref={containerRef} className={state === "data" ? "" : "hidden"} />
        {tooltip && state === "data" && (
          <div
            ref={tooltipElRef}
            className="absolute pointer-events-none z-20 rounded-md ring-1 ring-border/50 bg-popover/80 backdrop-blur-sm backdrop-saturate-200 px-3 py-2.5 text-xs leading-snug shadow-lg"
            style={{ left: tooltipLeft(tooltip, tooltipElRef.current), top: tooltip.top }}
          >
            <div className="font-semibold mb-1.5 text-foreground">{tooltip.time}</div>
            {tooltip.series.map((s) => (
              <div key={s.label} className="flex items-center gap-2 whitespace-nowrap">
                {s.dashed ? (
                  <span
                    className="w-3 shrink-0 border-t-2 border-dashed"
                    style={{ borderColor: s.color }}
                  />
                ) : (
                  <span
                    className="w-1 shrink-0 h-3 rounded-sm"
                    style={{ background: s.color }}
                  />
                )}
                <span className="text-muted-foreground">{s.label}</span>
                <span className="font-semibold ms-auto ps-4 text-foreground">{s.value}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
