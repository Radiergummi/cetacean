import type { TooltipData } from "./ChartTooltipOverlay";
import { formatMetricValue } from "@/lib/format.ts";
import type { ParsedMetrics } from "@/lib/metricsParser.ts";
import type { Chart as ChartJS, ChartData, ChartDataset } from "chart.js";

export interface Threshold {
  label: string;
  value: number;
  color: string;
  dash?: number[] | undefined;
}

/** Create a vertical gradient fill for a series color. */
function makeGradient(
  context: CanvasRenderingContext2D,
  chartArea: { top: number; bottom: number },
  color: string,
) {
  const gradient = context.createLinearGradient(0, chartArea.top, 0, chartArea.bottom);

  gradient.addColorStop(0, color + "30");
  gradient.addColorStop(1, color + "00");

  return gradient;
}

interface DatasetOptions {
  isolatedIndex: number | null;
  stacked: boolean;
  labelTransform?: ((label: string) => string) | undefined;
}

/**
 * Project parsed series onto the datasets Chart.js draws.
 *
 * Isolation is a drawing concern rather than a data one: the dimmed series stay
 * in the dataset list so their colours and legend positions hold still, and are
 * faded to a quarter alpha instead. Stacked is the exception — a stack sums what
 * it is given, so a dimmed series has to contribute zeroes or it would still
 * lift every series above it.
 */
export function buildChartDatasets(
  metrics: ParsedMetrics,
  { isolatedIndex, stacked, labelTransform }: DatasetOptions,
): ChartData<"line"> {
  return {
    labels: metrics.labels,
    datasets: metrics.series.map(({ color, data, label }, index): ChartDataset<"line"> => {
      const dimmed = isolatedIndex != null && isolatedIndex !== index;
      const base = {
        label: labelTransform ? labelTransform(label) : label,
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
          fill: "stack",
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
}

/**
 * The y-axis ceiling that keeps a threshold line on screen.
 *
 * Chart.js scales to the data, so a limit nobody is near would be drawn off the
 * top of the plot — which is the one moment it matters. Undefined when there is
 * no threshold to keep in view, leaving the axis to Chart.js.
 */
export function computeSuggestedMax(
  metrics: ParsedMetrics | null,
  thresholds: Threshold[] | undefined,
  yMin: number | undefined,
): number | undefined {
  if (!thresholds?.length || !metrics) {
    return undefined;
  }

  let high = Math.max(...metrics.series.flatMap(({ data }) => data));

  for (const threshold of thresholds) {
    high = Math.max(high, threshold.value);
  }

  const low = yMin ?? Math.min(...metrics.series.flatMap(({ data }) => data));

  return high + (high - low) * 0.1 || high + 1;
}

interface TooltipOptions {
  datasets: ChartDataset<"line">[];
  index: number;
  unit: string | undefined;
  thresholds: Threshold[] | undefined;
  /** The series behind the datasets, needed only to total a stack. */
  stackedSeries: ParsedMetrics["series"] | null;
}

/**
 * The rows a tooltip shows for one x position, largest first.
 *
 * Thresholds join the list so a value can be read against its limit without
 * looking away, and are marked dashed to match how they are drawn. A stack
 * leads with its total, since the visual height at that point is the sum and
 * no individual row states it.
 */
export function buildTooltipSeries({
  datasets,
  index,
  unit,
  thresholds,
  stackedSeries,
}: TooltipOptions): TooltipData["series"] {
  const items: TooltipData["series"] = [];

  for (const dataset of datasets) {
    const value = dataset.data[index] as number;

    if (value == null) {
      continue;
    }

    items.push({
      label: dataset.label ?? "value",
      color: dataset.borderColor as string,
      value: formatMetricValue(value, unit),
      raw: value,
    });
  }

  if (thresholds?.length) {
    for (const { color, label, value } of thresholds) {
      items.push({
        label,
        color,
        value: formatMetricValue(value, unit),
        raw: value,
        dashed: true,
      });
    }
  }

  items.sort((first, second) => second.raw - first.raw);

  if (stackedSeries) {
    const total = stackedSeries.reduce((sum, { data }) => sum + (data[index] ?? 0), 0);

    items.unshift({
      label: "Total",
      color: "transparent",
      value: formatMetricValue(total, unit),
      raw: total,
    });
  }

  return items;
}
