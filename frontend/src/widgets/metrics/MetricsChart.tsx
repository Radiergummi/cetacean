import type { MetricSeries } from "./types";
import { getChartColor } from "@/lib/chartColors";
import {
  CategoryScale,
  Chart as ChartJS,
  type ChartData,
  type ChartOptions,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
} from "chart.js";
import { Line } from "react-chartjs-2";

ChartJS.register(LineElement, PointElement, LinearScale, CategoryScale, Tooltip, Legend);

interface Props {
  series: MetricSeries[];
  formatValue: (value: number) => string;
}

/**
 * The Chart.js half of the metrics widget, kept apart so everything around it
 * can be tested — jsdom has no canvas, so a test that renders this renders
 * nothing at all.
 *
 * Colors come from the dashboard's own CVD-safe palette rather than a set
 * chosen here: the same metric must not be one colour in the dashboard and
 * another in a widget. Slots 1 and 3 (blue, magenta) are the pair used for
 * two-series metrics — their separation holds under every simulated colour
 * vision deficiency, where the neighbouring gold does not.
 */
export function MetricsChart({ formatValue, series }: Props) {
  const labels = series[0]?.points.map(({ time }) => formatTime(time)) ?? [];

  const data: ChartData<"line"> = {
    labels,
    datasets: series.map((entry, index) => ({
      label: entry.name,
      data: entry.points.map(({ value }) => value),
      borderColor: getChartColor(index === 0 ? 0 : 2),
      backgroundColor: getChartColor(index === 0 ? 0 : 2),
      borderWidth: 2,
      pointRadius: 0,
      pointHoverRadius: 4,
      tension: 0,
    })),
  };

  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    // A crosshair rather than a per-point hit: at a minute's resolution the
    // reader is asking "what happened here", not "what is this exact dot".
    interaction: { mode: "index", intersect: false },
    plugins: {
      // The legend is rendered beside the chart in text, with values.
      legend: { display: false },
      tooltip: {
        callbacks: {
          label: (context) => `${context.dataset.label}: ${formatValue(context.parsed.y ?? 0)}`,
        },
      },
    },
    scales: {
      x: {
        grid: { display: false },
        ticks: { maxTicksLimit: 6, font: { size: 10 } },
      },
      y: {
        beginAtZero: true,
        border: { display: false },
        grid: { color: "rgba(127,127,127,0.15)" },
        ticks: {
          maxTicksLimit: 5,
          font: { size: 10 },
          callback: (value) => formatValue(Number(value)),
        },
      },
    },
  };

  return (
    <Line
      data={data}
      options={options}
    />
  );
}

function formatTime(time: string): string {
  return new Date(time).toLocaleTimeString(undefined, {
    hour12: false,
    hour: "2-digit",
    minute: "2-digit",
  });
}
