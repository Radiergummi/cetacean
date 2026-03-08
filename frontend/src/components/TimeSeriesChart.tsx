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
}

type State = "loading" | "data" | "empty" | "error";

interface TooltipData {
  time: string;
  series: { label: string; color: string; value: string }[];
  left: number;
  top: number;
}

const RANGE_SECONDS: Record<string, number> = {
  "1h": 3600,
  "6h": 21600,
  "24h": 86400,
  "7d": 604800,
};

const CHART_COLORS = [
  "#6366f1", // indigo
  "#f59e0b", // amber
  "#10b981", // emerald
  "#ef4444", // red
  "#8b5cf6", // violet
  "#06b6d4", // cyan
];

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

export default function TimeSeriesChart({
  title,
  query,
  range,
  unit,
  refreshKey,
  thresholds,
  syncKey,
}: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);
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

                  ctx.fillStyle = t.color;
                  ctx.font = "10px sans-serif";
                  ctx.textAlign = "right";
                  ctx.fillText(
                    `${t.label}: ${formatValue(t.value, unit)}`,
                    plotLeft + plotWidth - 4,
                    yPos - 4,
                  );
                  ctx.restore();
                }
              },
            ],
          },
        };

        const tooltipPlugin: uPlot.Plugin = {
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
                const left = u.valToPos(ts, "x");
                const plotLeft = u.bbox.left / devicePixelRatio;
                const plotWidth = u.bbox.width / devicePixelRatio;
                tooltipRef.current({
                  time: new Date(ts * 1000).toLocaleTimeString(),
                  series: items,
                  left: plotLeft + (left > plotWidth / 2 ? left - 140 : left + 12),
                  top: u.bbox.top / devicePixelRatio + 8,
                });
              },
            ],
          },
        };

        const legendPlugin: uPlot.Plugin = {
          hooks: {
            init: [
              (u: uPlot) => {
                const labels = u.root.querySelectorAll(".u-legend .u-series");
                labels.forEach((el, i) => {
                  if (i === 0) return; // skip time series
                  el.addEventListener("click", () => {
                    const allHidden = u.series.slice(1).every((s, j) => j === i - 1 || !s.show);
                    for (let j = 1; j < u.series.length; j++) {
                      u.setSeries(j, { show: allHidden ? true : j === i });
                    }
                  });
                  (el as HTMLElement).style.cursor = "pointer";
                });
              },
            ],
          },
        };

        const opts: uPlot.Options = {
          width: containerRef.current.clientWidth || 600,
          height: 200,
          plugins: [thresholdPlugin, tooltipPlugin, legendPlugin],
          cursor: {
            drag: { x: false, y: false },
            sync: syncKey ? { key: syncKey, setSeries: true, scales: ["x", null] } : undefined,
            x: true,
            y: false,
          },
          focus: { alpha: 0.3 },
          series: [
            {},
            ...series.map((_s: any, i: number) => ({
              label: _s.metric?.__name__ || `series ${i + 1}`,
              stroke: CHART_COLORS[i % CHART_COLORS.length],
              width: 1.5,
              fill: CHART_COLORS[i % CHART_COLORS.length] + "1a",
            })),
          ],
          axes: [
            {
              stroke: "#888",
              grid: { stroke: "#88888820" },
              ticks: { stroke: "#88888820" },
            },
            {
              stroke: "#888",
              grid: { stroke: "#88888820" },
              ticks: { stroke: "#88888820" },
              values: (_u: uPlot, vals: number[]) => vals.map((v) => formatValue(v, unit)),
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
    <div className="rounded-lg border bg-card p-4">
      <div className="flex items-center justify-between mb-2">
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
            className="absolute pointer-events-none z-20 rounded-md border bg-popover px-3 py-2 text-xs shadow-md"
            style={{ left: tooltip.left, top: tooltip.top }}
          >
            <div className="text-muted-foreground mb-1">{tooltip.time}</div>
            {tooltip.series.map((s) => (
              <div key={s.label} className="flex items-center gap-1.5">
                <span
                  className="inline-block w-2 h-2 rounded-full shrink-0"
                  style={{ background: s.color }}
                />
                <span>
                  {s.label}: <b>{s.value}</b>
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
