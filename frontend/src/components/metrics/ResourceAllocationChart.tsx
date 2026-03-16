import { getChartColor } from "../../lib/chartColors";
import { formatMetricValue } from "../../lib/formatMetricValue";
import {
  Chart as ChartJS,
  BarElement,
  CategoryScale,
  LinearScale,
  type ChartOptions,
  type Plugin,
} from "chart.js";
import { useMemo, useRef } from "react";
import { Bar } from "react-chartjs-2";

ChartJS.register(BarElement, CategoryScale, LinearScale);

interface BarChartProps {
  title: string;
  reserved?: number;
  actual?: number;
  limit?: number;
  unit: "%" | "bytes";
}

function AllocationBar({ title, reserved, actual, limit, unit }: BarChartProps) {
  const color = getChartColor(0);
  const limitRef = useRef(limit);
  limitRef.current = limit;

  const chartData = useMemo(() => {
    const labels: string[] = [];
    const values: number[] = [];
    const colors: string[] = [];
    if (reserved != null) {
      labels.push("Reserved");
      values.push(reserved);
      colors.push(color + "4D");
    }
    if (actual != null) {
      labels.push("Actual");
      values.push(actual);
      colors.push(color);
    }
    return {
      labels,
      datasets: [{ data: values, backgroundColor: colors, borderRadius: 4, barThickness: 20 }],
    };
  }, [reserved, actual, color]);

  const limitPlugin = useMemo<Plugin<"bar">>(
    () => ({
      id: "limitMarker",
      afterDatasetsDraw(chart) {
        const lim = limitRef.current;
        if (lim == null) return;
        const { ctx, chartArea, scales } = chart;
        if (!chartArea || !scales.x) return;
        const xPixel = scales.x.getPixelForValue(lim);
        if (xPixel < chartArea.left || xPixel > chartArea.right) return;
        ctx.save();
        ctx.strokeStyle =
          getComputedStyle(document.documentElement).getPropertyValue("--destructive").trim() ||
          "#ef4444";
        ctx.lineWidth = 2;
        ctx.setLineDash([6, 4]);
        ctx.beginPath();
        ctx.moveTo(xPixel, chartArea.top);
        ctx.lineTo(xPixel, chartArea.bottom);
        ctx.stroke();
        ctx.restore();
      },
    }),
    [],
  );

  const maxVal = Math.max(reserved ?? 0, actual ?? 0, limit ?? 0) * 1.15;

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      indexAxis: "y" as const,
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (ctx) => ` ${formatMetricValue(ctx.parsed.x ?? 0, unit)}`,
          },
        },
      },
      scales: {
        x: {
          display: false,
          min: 0,
          max: maxVal,
        },
        y: {
          grid: { display: false },
          ticks: {
            font: { size: 11 },
          },
        },
      },
    }),
    [unit, maxVal],
  );

  const plugins = useMemo(() => [limitPlugin], [limitPlugin]);

  if (reserved == null && actual == null) return null;

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <span className="text-sm font-medium">{title}</span>
        {limit != null && (
          <span className="text-[11px] text-muted-foreground">
            Limit: {formatMetricValue(limit, unit)}
          </span>
        )}
      </div>
      <div className="h-[80px]">
        <Bar
          data={chartData}
          options={options}
          plugins={plugins}
        />
      </div>
    </div>
  );
}

interface Props {
  cpuReserved?: number;
  cpuLimit?: number;
  cpuActual?: number;
  memReserved?: number;
  memLimit?: number;
  memActual?: number;
}

export default function ResourceAllocationChart({
  cpuReserved,
  cpuLimit,
  cpuActual,
  memReserved,
  memLimit,
  memActual,
}: Props) {
  const hasCpu = cpuReserved != null || cpuActual != null || cpuLimit != null;
  const hasMem = memReserved != null || memActual != null || memLimit != null;

  if (!hasCpu && !hasMem) return null;

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      {hasCpu && (
        <div className="rounded-lg border bg-card p-4">
          <AllocationBar
            title="CPU"
            reserved={cpuReserved}
            actual={cpuActual}
            limit={cpuLimit}
            unit="%"
          />
        </div>
      )}
      {hasMem && (
        <div className="rounded-lg border bg-card p-4">
          <AllocationBar
            title="Memory"
            reserved={memReserved}
            actual={memActual}
            limit={memLimit}
            unit="bytes"
          />
        </div>
      )}
    </div>
  );
}
