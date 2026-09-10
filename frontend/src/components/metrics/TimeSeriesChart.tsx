// oxlint-disable react/refs -- Chart.js plugins are memoized once and read
// live state through refs so their callbacks never close over a stale render.
// That is the documented architecture for this chart (crosshair sync,
// click-to-isolate, brush-to-zoom); the reads happen inside plugin callbacks
// that Chart.js invokes outside React's render, not during render itself.
import {
  buildChartDatasets,
  buildTooltipSeries,
  computeSuggestedMax,
  type Threshold,
} from "./chartDatasets";
import { useChartSync } from "./ChartSyncProvider";
import ChartTooltipOverlay, { type TooltipData } from "./ChartTooltipOverlay";
import { useMetricsPanelContext } from "./MetricsPanelContext";
import { useMetricsSeries } from "./useMetricsSeries";
import { useSeriesIsolation } from "./useSeriesIsolation";
import { useMatchesBreakpoint } from "@/hooks/useMatchesBreakpoint.ts";
import { getSemanticChartColor } from "@/lib/chartColors.ts";
import {
  CategoryScale,
  Chart as ChartJS,
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
import { useEffect, useId, useMemo, useRef, useState } from "react";
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

export type { Threshold } from "./chartDatasets";

interface Props {
  title: string;
  query: string;
  range: string;
  unit?: string | undefined;
  refreshKey?: number | undefined;
  thresholds?: Threshold[] | undefined;
  /** Force y-axis minimum value (e.g., 0 to always start at zero). */
  yMin?: number | undefined;
  /** Override the default series color. */
  color?: string | undefined;
  from?: number | undefined;
  to?: number | undefined;
  onRangeSelect?: ((from: number, to: number) => void) | undefined;
  onSeriesDoubleClick?: ((seriesLabel: string) => void) | undefined;
  onSeriesInfo?: ((series: { label: string; color: string }[]) => void) | undefined;
  stackable?: boolean | undefined;
  /** Controlled isolation: label of the isolated series, or null for none. */
  isolatedLabel?: string | null | undefined;
  /** Fires when isolation changes (from chart clicks or sync). */
  onIsolationChange?: ((label: string | null) => void) | undefined;
  /** Transform series labels for display (tooltips, legend). Raw labels are still used for identification. */
  labelTransform?: ((label: string) => string) | undefined;
}

/** How long a click waits to see whether it is the first half of a double-click. */
const clickSettleMilliseconds = 250;

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
  labelTransform,
}: Props) {
  const isMobile = useMatchesBreakpoint("md", "below");
  const chartRef = useRef<ChartJS<"line"> | null>(null);
  const [tooltip, setTooltip] = useState<TooltipData | null>(null);
  const chartId = useId();
  const sync = useChartSync();
  const panel = useMetricsPanelContext();

  const [localStacked, setLocalStacked] = useState(false);
  const stacked = panel?.stacked ?? localStacked;

  const { state, errorMessage, data, refetch } = useMetricsSeries({
    query,
    range,
    title,
    unit,
    color: colorOverride,
    from,
    to,
    refreshKey,
    streaming: panel?.streaming ?? true,
    onSeriesInfo,
  });

  const isolation = useSeriesIsolation({
    chartId,
    data,
    isolatedLabel,
    onIsolationChange,
  });

  const tooltipRef = useRef(setTooltip);
  tooltipRef.current = setTooltip;
  const unitRef = useRef(unit);
  unitRef.current = unit;
  const thresholdsRef = useRef(thresholds);
  thresholdsRef.current = thresholds;
  const dataRef = useRef(data);
  dataRef.current = data;
  const stackedRef = useRef(stacked);
  stackedRef.current = stacked;
  const isolationRef = useRef(isolation);
  isolationRef.current = isolation;
  const onSeriesDoubleClickRef = useRef(onSeriesDoubleClick);
  onSeriesDoubleClickRef.current = onSeriesDoubleClick;
  const onRangeSelectRef = useRef(onRangeSelect);
  onRangeSelectRef.current = onRangeSelect;

  const justZoomedRef = useRef(false);
  const clickTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const syncIndexRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (clickTimerRef.current) {
        clearTimeout(clickTimerRef.current);
        clickTimerRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    return sync.subscribe(chartId, (timestamp) => {
      const current = dataRef.current;

      syncIndexRef.current =
        timestamp > 0 && current
          ? current.timestamps.findIndex((candidate) => candidate >= timestamp)
          : null;

      chartRef.current?.draw();
    });
  }, [chartId, sync]);

  const chartData = useMemo(() => {
    if (!data) {
      return null;
    }

    return buildChartDatasets(data, { isolatedIndex: isolation.index, stacked, labelTransform });
  }, [data, isolation.index, stacked, labelTransform]);

  const suggestedMax = useMemo(
    () => computeSuggestedMax(data, thresholds, yMin),
    [data, thresholds, yMin],
  );

  const thresholdPlugin = useMemo<Plugin<"line">>(
    () => ({
      id: "thresholdLines",
      afterDatasetsDraw({ ctx, chartArea, scales }) {
        const lines = thresholdsRef.current;
        const yScale = scales["y"];

        if (!lines?.length || !yScale || !chartArea) {
          return;
        }

        for (const { color, dash, value } of lines) {
          const yPosition = yScale.getPixelForValue(value);

          if (yPosition < chartArea.top || yPosition > chartArea.top + chartArea.height) {
            continue;
          }

          ctx.save();
          ctx.strokeStyle = color;
          ctx.lineWidth = 1.5;

          if (dash) {
            ctx.setLineDash(dash);
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
      afterEvent(chart, { event: { native, type, x } }) {
        if (type === "mouseout") {
          tooltipRef.current(null);
          sync.publish(chartId, -1);
          chart.draw();

          return;
        }

        const nearest = () =>
          chart.getElementsAtEventForMode(native as Event, "nearest", { intersect: false }, false);

        if (type === "dblclick") {
          if (clickTimerRef.current) {
            clearTimeout(clickTimerRef.current);

            clickTimerRef.current = null;
          }

          const doubleClicked = nearest()[0];

          if (!doubleClicked) {
            return;
          }

          const label = chart.data.datasets[doubleClicked.datasetIndex]?.label;

          if (label) {
            onSeriesDoubleClickRef.current?.(label);
          }

          return;
        }

        if (type === "click") {
          // A drag that ended in a zoom also arrives as a click. Swallow it, or
          // brushing a range would isolate whatever sat under the release.
          if (justZoomedRef.current) {
            justZoomedRef.current = false;

            return;
          }

          if (x == null) {
            return;
          }

          const elements = nearest();

          if (clickTimerRef.current) {
            clearTimeout(clickTimerRef.current);
          }

          clickTimerRef.current = setTimeout(() => {
            clickTimerRef.current = null;

            const clicked = elements[0];

            if (!clicked) {
              isolationRef.current.isolate(null);

              return;
            }

            const alreadyIsolated = isolationRef.current.index === clicked.datasetIndex;

            isolationRef.current.isolate(alreadyIsolated ? null : clicked.datasetIndex);
          }, clickSettleMilliseconds);

          return;
        }

        if (type !== "mousemove" || x == null) {
          return;
        }

        const { chartArea, scales } = chart;
        const xScale = scales["x"];

        if (!chartArea || !xScale) {
          return;
        }

        if (x < chartArea.left || x > chartArea.right) {
          tooltipRef.current(null);

          return;
        }

        const xValue = xScale.getValueForPixel(x);

        if (xValue == null) {
          return;
        }

        const index = Math.round(xValue);
        const datasets = chart.data.datasets;

        if (index < 0 || index >= (datasets[0]?.data?.length ?? 0)) {
          return;
        }

        const current = dataRef.current;
        const timestamp = current?.timestamps[index];

        tooltipRef.current({
          time: timestamp ? new Date(timestamp * 1000).toLocaleTimeString() : "",
          series: buildTooltipSeries({
            datasets,
            index,
            unit: unitRef.current,
            thresholds: thresholdsRef.current,
            stackedSeries: stackedRef.current ? (current?.series ?? null) : null,
          }),
          x,
          chartWidth: chartArea.right,
          top: chartArea.top + 8,
        });

        if (timestamp != null) {
          sync.publish(chartId, timestamp);
        }
      },
      afterDraw(chart) {
        // Read through `chart` rather than destructuring: getActiveElements is a
        // method and reads `this._active`, so a loose reference throws on the
        // first draw. Tests that mock the canvas away cannot see that.
        const { ctx, chartArea, scales } = chart;

        if (!chartArea) {
          return;
        }

        const hovered = chart.getActiveElements()[0];

        if (hovered) {
          ctx.save();
          ctx.beginPath();
          ctx.moveTo(hovered.element.x, chartArea.top);
          ctx.lineTo(hovered.element.x, chartArea.bottom);
          ctx.lineWidth = 1;
          ctx.strokeStyle = getSemanticChartColor("crosshair");
          ctx.stroke();
          ctx.restore();
        }

        // The dashed twin, drawn where a sibling chart is being hovered.
        const syncIndex = syncIndexRef.current;

        if (syncIndex == null || syncIndex < 0) {
          return;
        }

        const xPixel = scales["x"]?.getPixelForValue(syncIndex);

        if (xPixel == null || xPixel < chartArea.left || xPixel > chartArea.right) {
          return;
        }

        ctx.save();
        ctx.beginPath();
        ctx.moveTo(xPixel, chartArea.top);
        ctx.lineTo(xPixel, chartArea.bottom);
        ctx.lineWidth = 1;
        ctx.strokeStyle = getSemanticChartColor("crosshair");
        ctx.setLineDash([4, 4]);
        ctx.stroke();

        const yScale = scales["y"];

        if (yScale) {
          for (const dataset of chart.data.datasets) {
            const value = dataset.data[syncIndex] as number | null | undefined;

            if (value == null) {
              continue;
            }

            ctx.beginPath();
            ctx.arc(xPixel, yScale.getPixelForValue(value), 3, 0, Math.PI * 2);
            ctx.fillStyle = dataset.borderColor as string;
            ctx.fill();
          }
        }

        ctx.restore();
      },
    }),
    [chartId, sync],
  );

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      // dblclick is not in Chart.js' type union but is dispatched by the browser canvas
      events: [
        "mousemove",
        "mouseout",
        "click",
        "dblclick",
        "touchstart",
        "touchmove",
      ] as unknown as NonNullable<ChartOptions<"line">["events"]>,
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

              const current = dataRef.current;
              const callback = onRangeSelectRef.current;
              const xScale = chart.scales["x"];

              if (!callback || !current || !xScale) {
                return;
              }

              const first = current.timestamps[Math.max(0, Math.floor(xScale.min))];
              const last =
                current.timestamps[Math.min(current.timestamps.length - 1, Math.ceil(xScale.max))];

              if (first && last) {
                callback(first, last);
              }

              // The range change re-fetches; the zoom itself is only the gesture.
              chart.resetZoom();
            },
          },
        },
      },
      scales: {
        x: { display: false },
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
          <div className="ms-1 flex items-center gap-0.5">
            <button
              type="button"
              onClick={() => setLocalStacked(false)}
              aria-pressed={!stacked}
              className="rounded p-0.5 hover:bg-muted/50 aria-pressed:bg-muted"
              title="Line chart"
            >
              <LineChart className="size-3.5" />
            </button>
            <button
              type="button"
              onClick={() => setLocalStacked(true)}
              aria-pressed={stacked}
              className="rounded p-0.5 hover:bg-muted/50 aria-pressed:bg-muted"
              title="Stacked area"
            >
              <AreaChart className="size-3.5" />
            </button>
          </div>
        )}

        {unit && <span className="ms-auto text-xs text-muted-foreground">{unit}</span>}
      </div>

      {state === "loading" && !data && <div className="h-50 rounded bg-muted/50" />}

      {state === "error" && (
        <div className="flex h-50 items-center justify-center rounded border border-destructive/20 bg-destructive/5">
          <div className="text-center">
            <p className="mb-2 text-sm text-destructive">{errorMessage}</p>
            <button
              type="button"
              onClick={refetch}
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
        {state === "loading" && data && (
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
        <ChartTooltipOverlay
          tooltip={tooltip}
          visible={state === "data"}
        />
      </div>
    </div>
  );
}
