import { api } from "../api/client";
import type { DiskUsageSummary } from "../api/types";
import { getChartColor } from "../lib/chartColors";
import { chartTooltipClasses } from "../lib/chartTooltip";
import { formatBytes } from "../lib/format";
import CollapsibleSection from "./CollapsibleSection";
import { useQuery } from "@tanstack/react-query";
import { ArcElement, Chart as ChartJS, Tooltip } from "chart.js";
import { Box, Container, Hammer, HardDrive, type LucideIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Doughnut } from "react-chartjs-2";

ChartJS.register(ArcElement, Tooltip);

const typeMeta: Record<string, { label: string; icon: LucideIcon }> = {
  images: { label: "Images", icon: HardDrive },
  containers: { label: "Containers", icon: Container },
  volumes: { label: "Volumes", icon: Box },
  buildCache: { label: "Build Cache", icon: Hammer },
};

function buildColorMap(sorted: DiskUsageSummary[]): Record<string, string> {
  const map: Record<string, string> = {};

  sorted.forEach(({ type }, index) => {
    map[type] = getChartColor(index);
  });

  return map;
}

function reclaimableCell(reclaimable: number, total: number) {
  if (reclaimable <= 0) {
    return "0 B";
  }

  const percentage = total > 0 ? Math.round((reclaimable / total) * 100) : 0;

  return (
    <>
      {formatBytes(reclaimable)} <span className="ms-1">({percentage}%)</span>
    </>
  );
}

/** Ensure very small slices are still visible by enforcing a minimum display value. */
function withMinSlice(values: number[], total?: number): number[] {
  const sum = total ?? values.reduce((sum, value) => sum + value, 0);

  if (sum === 0) {
    return values.map(() => 0);
  }

  const minFraction = 0.01;

  return values.map((value) => Math.max(value, sum * minFraction));
}

function buildTooltipElement(
  color: string,
  label: string,
  size: string,
  pctText: string,
): HTMLDivElement {
  const element = document.createElement("div");

  const row = document.createElement("div");
  row.className = "flex items-center gap-2 whitespace-nowrap";

  const swatch = document.createElement("span");
  swatch.className = "w-1 shrink-0 h-3 rounded-sm";
  swatch.style.background = color;

  const labelSpan = document.createElement("span");
  labelSpan.className = "text-muted-foreground";
  labelSpan.textContent = label;

  const valueSpan = document.createElement("span");
  valueSpan.className = "font-semibold ms-auto ps-4 text-foreground";
  valueSpan.textContent = size;

  row.append(swatch, labelSpan, valueSpan);

  const pctRow = document.createElement("div");
  pctRow.className = "text-muted-foreground mt-0.5 text-right";
  pctRow.textContent = pctText;

  element.append(row, pctRow);

  return element;
}

function externalTooltipHandler(context: {
  chart: ChartJS;
  tooltip: {
    opacity: number;
    dataPoints?: { dataIndex: number; datasetIndex: number }[];
    caretX: number;
    caretY: number;
  };
}) {
  const { chart, tooltip: model } = context;
  const canvas = chart.canvas;
  let element = canvas.parentElement?.querySelector<HTMLDivElement>(".chartjs-tooltip");

  if (!element) {
    element = document.createElement("div");
    element.className = `chartjs-tooltip ${chartTooltipClasses}`;
    canvas.parentElement?.appendChild(element);
  }

  element.style.transition = "opacity 100ms ease";

  if (model.opacity === 0 || !model.dataPoints?.length) {
    element.style.opacity = "0";

    return;
  }

  const index = model.dataPoints[0].dataIndex;
  const rawData = (chart.data.datasets[0] as unknown as { _rawData: DiskUsageSummary[] })._rawData;

  if (!rawData?.[index]) {
    return;
  }

  const { reclaimable, totalSize, type } = rawData[index];
  const total = rawData.reduce((sum, { totalSize: size }) => sum + size, 0);
  const color = getChartColor(index);
  const label = typeMeta[type]?.label ?? type;
  const size = formatBytes(totalSize);
  const percentage = total > 0 ? Math.round((totalSize / total) * 100) : 0;
  const percentageText = `${percentage}% of total`;

  const node = buildTooltipElement(color, label, size, percentageText);

  // Add reclaimable info line
  if (reclaimable > 0) {
    const reclaimRow = document.createElement("div");
    reclaimRow.className = "text-muted-foreground mt-0.5 text-right text-[10px]";
    const reclaimPct = Math.round((reclaimable / totalSize) * 100);
    reclaimRow.textContent = `${formatBytes(reclaimable)} reclaimable (${reclaimPct}%)`;
    node.appendChild(reclaimRow);
  }

  element.replaceChildren(node);

  Object.assign(element.style, {
    opacity: "1",
    left: model.caretX + 12 + "px",
    top: model.caretY - 10 + "px",
  });
}

function DoughnutChart({ data }: { data: DiskUsageSummary[] }) {
  const total = useMemo(() => data.reduce((sum, { totalSize }) => sum + totalSize, 0), [data]);

  const chartData = useMemo(
    () => ({
      labels: data.map(({ type }) => typeMeta[type]?.label ?? type),
      datasets: [
        {
          data: withMinSlice(data.map(({ totalSize }) => totalSize)),
          backgroundColor: data.map((_, index) => getChartColor(index)),
          borderWidth: 0,
          borderRadius: 4,
          spacing: 3,
          hoverOffset: 8,
          _rawData: data,
        },
      ],
    }),
    [data],
  );

  const centerTextPlugin = useMemo(
    () => ({
      id: "centerText",
      afterDraw(chart: ChartJS) {
        const { ctx, width, height } = chart;
        const size = Math.min(width, height);
        ctx.save();

        const text = formatBytes(total);
        const mainSize = Math.round(size * 0.08);
        ctx.font = `600 ${mainSize}px ui-sans-serif, system-ui, sans-serif`;
        ctx.fillStyle = getComputedStyle(chart.canvas).color;
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.fillText(text, width / 2, height / 2 - mainSize * 0.45);

        const subSize = Math.round(size * 0.06);
        ctx.font = `${subSize}px ui-sans-serif, system-ui, sans-serif`;
        ctx.globalAlpha = 0.5;
        ctx.fillText("Total", width / 2, height / 2 + mainSize * 0.6);

        ctx.restore();
      },
    }),
    [total],
  );

  const options = useMemo(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      cutout: "62%",
      layout: { padding: 10 },
      plugins: {
        legend: { display: false },
        tooltip: {
          enabled: false,
          external: externalTooltipHandler,
        },
      },
    }),
    [],
  );

  const plugins = useMemo(() => [centerTextPlugin], [centerTextPlugin]);

  if (total === 0) {
    return null;
  }

  return (
    <div className="relative h-72 w-full">
      <Doughnut
        data={chartData}
        options={options}
        plugins={plugins}
      />
    </div>
  );
}

function DiskUsageTable({ data }: { data: DiskUsageSummary[] }) {
  const sorted = [...data].sort((a, b) => b.totalSize - a.totalSize);
  const colorMap = buildColorMap(sorted);
  const total = data.reduce((sum, { totalSize }) => sum + totalSize, 0);
  const reclaimable = data.reduce((sum, { reclaimable }) => sum + reclaimable, 0);

  return (
    <div className="flex items-center gap-4">
      <div className="flex-1 overflow-x-auto rounded-lg border bg-card">
        <table className="w-full min-w-max text-sm whitespace-nowrap">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="p-3 text-left font-medium">Type</th>
              <th className="p-3 text-right font-medium">Count</th>
              <th className="p-3 text-right font-medium">Active</th>
              <th className="p-3 text-right font-medium">Size</th>
              <th className="p-3 text-right font-medium">Reclaimable</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map(({ active, count, reclaimable, totalSize, type }) => {
              const meta = typeMeta[type];
              const Icon = meta?.icon;
              const color = colorMap[type];
              return (
                <tr
                  key={type}
                  className="border-b last:border-b-0"
                >
                  <td className="p-3">
                    <span className="inline-flex items-center gap-2">
                      {Icon && (
                        <Icon
                          className="size-4"
                          style={{ color }}
                        />
                      )}
                      {meta?.label ?? type}
                    </span>
                  </td>
                  <td className="p-3 text-right tabular-nums">{count}</td>
                  <td className="p-3 text-right tabular-nums">{active}</td>
                  <td className="p-3 text-right tabular-nums">{formatBytes(totalSize)}</td>
                  <td className="p-3 text-right text-muted-foreground tabular-nums">
                    {reclaimableCell(reclaimable, totalSize)}
                  </td>
                </tr>
              );
            })}
          </tbody>
          {total > 0 && (
            <tfoot>
              <tr className="border-t bg-muted/30">
                <td className="p-3 font-medium">Total</td>
                <td className="p-3" />
                <td className="p-3" />
                <td className="p-3 text-right font-medium tabular-nums">{formatBytes(total)}</td>
                <td className="p-3 text-right font-medium text-muted-foreground tabular-nums">
                  {reclaimableCell(reclaimable, total)}
                </td>
              </tr>
            </tfoot>
          )}
        </table>
      </div>
      <div className="flex max-h-56 w-1/3 items-center justify-center">
        <DoughnutChart data={sorted} />
      </div>
    </div>
  );
}

function DiskUsageLoading() {
  return (
    <div className="flex items-stretch gap-4">
      <div className="flex-1 overflow-x-auto rounded-lg border bg-card">
        <table className="w-full min-w-max text-sm whitespace-nowrap">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="p-3 text-left font-medium">Type</th>
              <th className="p-3 text-right font-medium">Count</th>
              <th className="p-3 text-right font-medium">Active</th>
              <th className="p-3 text-right font-medium">Size</th>
              <th className="p-3 text-right font-medium">Reclaimable</th>
            </tr>
          </thead>
          <tbody>
            {[1, 2, 3, 4].map((index) => (
              <tr
                key={index}
                className="border-b last:border-b-0"
              >
                <td className="p-3">
                  <div className="h-4 w-24 animate-pulse rounded bg-muted" />
                </td>
                <td className="p-3">
                  <div className="ms-auto h-4 w-8 animate-pulse rounded bg-muted" />
                </td>
                <td className="p-3">
                  <div className="ms-auto h-4 w-8 animate-pulse rounded bg-muted" />
                </td>
                <td className="p-3">
                  <div className="ms-auto h-4 w-16 animate-pulse rounded bg-muted" />
                </td>
                <td className="p-3">
                  <div className="ms-auto h-4 w-24 animate-pulse rounded bg-muted" />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex w-1/3 shrink-0 items-center justify-center">
        <div className="size-30 animate-pulse rounded-full bg-muted" />
      </div>
    </div>
  );
}

/**
 * When `nodeId` is provided, only renders if the node matches the Docker host
 * Cetacean is connected to (disk usage data is local to that host).
 */
export default function DiskUsageSection({ nodeId }: { nodeId?: string }) {
  const [visible, setVisible] = useState(!nodeId);
  const [loading, setLoading] = useState(!!nodeId);

  useEffect(() => {
    if (!nodeId) {
      return;
    }

    api
      .cluster()
      .then(({ localNodeID }) => {
        if (localNodeID && localNodeID === nodeId) {
          setVisible(true);
        } else {
          setLoading(false);
        }
      })
      .catch(() => setLoading(false));
  }, [nodeId]);

  const { data, isLoading: diskLoading } = useQuery({
    queryKey: ["disk-usage"],
    queryFn: () => api.diskUsage(),
    enabled: visible,
  });

  const isLoading = loading || diskLoading;

  if (!visible || (!isLoading && !data)) {
    return null;
  }

  return (
    <CollapsibleSection title="Docker Disk Usage">
      {data ? <DiskUsageTable data={data} /> : <DiskUsageLoading />}
    </CollapsibleSection>
  );
}
