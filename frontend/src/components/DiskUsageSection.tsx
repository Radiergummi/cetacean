import {ArcElement, Chart as ChartJS, Tooltip} from "chart.js";
import {Box, Container, Hammer, HardDrive, type LucideIcon} from "lucide-react";
import {useEffect, useMemo, useState} from "react";
import {Doughnut} from "react-chartjs-2";
import {api} from "../api/client";
import type {DiskUsageSummary} from "../api/types";
import {formatBytes} from "../lib/formatBytes";
import {getChartColor} from "../lib/chartColors";
import {CHART_TOOLTIP_CLASS} from "../lib/chartTooltip";
import CollapsibleSection from "./CollapsibleSection";

ChartJS.register(ArcElement, Tooltip);


const typeMeta: Record<string, { label: string; icon: LucideIcon }> = {
    images: {label: "Images", icon: HardDrive},
    containers: {label: "Containers", icon: Container},
    volumes: {label: "Volumes", icon: Box},
    buildCache: {label: "Build Cache", icon: Hammer},
};

function buildColorMap(sorted: DiskUsageSummary[]): Record<string, string> {
    const map: Record<string, string> = {};
    sorted.forEach((d, i) => {
        map[d.type] = getChartColor(i);
    });
    return map;
}

function reclaimableCell(reclaimable: number, total: number) {
    if (reclaimable <= 0) {
        return "0 B";
    }
    const pct = total > 0 ? Math.round((
        reclaimable / total
    ) * 100) : 0;
    return (
        <>
            {formatBytes(reclaimable)} <span className="ml-1">({pct}%)</span>
        </>
    );
}

/** Ensure very small slices are still visible by enforcing a minimum display value. */
function withMinSlice(values: number[], total?: number): number[] {
    const sum = total ?? values.reduce((s, v) => s + v, 0);
    if (sum === 0) return values.map(() => 0);
    const minFraction = 0.04;
    return values.map((v) => Math.max(v, sum * minFraction));
}

function buildTooltipEl(color: string, label: string, size: string, pctText: string): HTMLDivElement {
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
    tooltip: { opacity: number; dataPoints?: { dataIndex: number; datasetIndex: number }[]; caretX: number; caretY: number };
}) {
    const {chart, tooltip: model} = context;
    const canvas = chart.canvas;
    let element = canvas.parentElement?.querySelector<HTMLDivElement>(".chartjs-tooltip");

    if (!element) {
        element = document.createElement("div");
        element.className =
            `chartjs-tooltip ${CHART_TOOLTIP_CLASS}`;
        canvas.parentElement?.appendChild(element);
    }

    if (model.opacity === 0 || !model.dataPoints?.length) {
        element.style.opacity = "0";
        return;
    }

    const idx = model.dataPoints[0].dataIndex;
    const rawData = (chart.data.datasets[0] as unknown as { _rawData: DiskUsageSummary[] })._rawData;
    if (!rawData?.[idx]) return;

    const d = rawData[idx];
    const total = rawData.reduce((sum, r) => sum + r.totalSize, 0);
    const color = getChartColor(idx);
    const label = typeMeta[d.type]?.label ?? d.type;
    const size = formatBytes(d.totalSize);
    const pct = total > 0 ? Math.round((d.totalSize / total) * 100) : 0;
    const pctText = `${pct}% of total`;

    const el = buildTooltipEl(color, label, size, pctText);

    // Add reclaimable info line
    if (d.reclaimable > 0) {
        const reclaimRow = document.createElement("div");
        reclaimRow.className = "text-muted-foreground mt-0.5 text-right text-[10px]";
        const reclaimPct = Math.round((d.reclaimable / d.totalSize) * 100);
        reclaimRow.textContent = `${formatBytes(d.reclaimable)} reclaimable (${reclaimPct}%)`;
        el.appendChild(reclaimRow);
    }

    element.replaceChildren(el);

    Object.assign(element.style, {
        opacity: "1",
        left: model.caretX + 12 + "px",
        top: model.caretY - 10 + "px",
        transition: "opacity 50ms ease",
    });
}

function DoughnutChart({data}: { data: DiskUsageSummary[] }) {
    const total = useMemo(() => data.reduce((sum, {totalSize}) => sum + totalSize, 0), [data]);

    const chartData = useMemo(() => ({
        labels: data.map((d) => typeMeta[d.type]?.label ?? d.type),
        datasets: [{
            data: withMinSlice(data.map((d) => d.totalSize)),
            backgroundColor: data.map((_, i) => getChartColor(i)),
            borderWidth: 0,
            borderRadius: 4,
            spacing: 3,
            hoverOffset: 8,
            _rawData: data,
        }],
    }), [data]);

    const centerTextPlugin = useMemo(
        () => (
            {
                id: "centerText",
                afterDraw(chart: ChartJS) {
                    const {ctx, width, height} = chart;
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
            }
        ),
        [total],
    );

    const options = useMemo(
        () => (
            {
                responsive: true,
                maintainAspectRatio: true,
                cutout: "62%",
                plugins: {
                    legend: {display: false},
                    tooltip: {
                        enabled: false,
                        external: externalTooltipHandler,
                    },
                },
            }
        ),
        [],
    );

    const plugins = useMemo(() => [centerTextPlugin], [centerTextPlugin]);

    if (total === 0) return null;

    return (
        <div className="relative flex items-center justify-center h-full w-full overflow-visible">
            <Doughnut data={chartData} options={options} plugins={plugins}/>
        </div>
    );
}

function DiskUsageTable({data}: { data: DiskUsageSummary[] }) {
    const sorted = [...data].sort((a, b) => b.totalSize - a.totalSize);
    const colorMap = buildColorMap(sorted);
    const total = data.reduce((sum, {totalSize}) => sum + totalSize, 0);
    const reclaimable = data.reduce((sum, {reclaimable}) => sum + reclaimable, 0);

    return (
        <div className="flex items-stretch gap-4">
            <div className="flex-1 rounded-lg border bg-card overflow-x-auto">
                <table className="w-full min-w-max whitespace-nowrap text-sm">
                    <thead>
                    <tr className="border-b bg-muted/50">
                        <th className="text-left p-3 font-medium">Type</th>
                        <th className="text-right p-3 font-medium">Count</th>
                        <th className="text-right p-3 font-medium">Active</th>
                        <th className="text-right p-3 font-medium">Size</th>
                        <th className="text-right p-3 font-medium">Reclaimable</th>
                    </tr>
                    </thead>
                    <tbody>
                    {sorted.map((d) => {
                        const meta = typeMeta[d.type];
                        const Icon = meta?.icon;
                        const color = colorMap[d.type];
                        return (
                            <tr key={d.type} className="border-b last:border-b-0">
                                <td className="p-3">
                                    <span className="inline-flex items-center gap-2">
                                        {Icon && <Icon className="size-4" style={{color}}/>}
                                        {meta?.label ?? d.type}
                                    </span>
                                </td>
                                <td className="p-3 text-right tabular-nums">{d.count}</td>
                                <td className="p-3 text-right tabular-nums">{d.active}</td>
                                <td className="p-3 text-right tabular-nums">
                                    {d.totalSize > 0 ? formatBytes(d.totalSize) : "0 B"}
                                </td>
                                <td className="p-3 text-right tabular-nums text-muted-foreground">
                                    {reclaimableCell(d.reclaimable, d.totalSize)}
                                </td>
                            </tr>
                        );
                    })}
                    </tbody>
                    {total > 0 && (
                        <tfoot>
                        <tr className="border-t bg-muted/30">
                            <td className="p-3 font-medium">Total</td>
                            <td className="p-3"/>
                            <td className="p-3"/>
                            <td className="p-3 text-right tabular-nums font-medium">{formatBytes(total)}</td>
                            <td className="p-3 text-right tabular-nums text-muted-foreground font-medium">
                                {reclaimableCell(reclaimable, total)}
                            </td>
                        </tr>
                        </tfoot>
                    )}
                </table>
            </div>
            <div className="w-1/3 shrink-0 flex items-center justify-center">
                <DoughnutChart data={sorted}/>
            </div>
        </div>
    );
}

function DiskUsageLoading() {
    return (
        <div className="flex items-stretch gap-4">
            <div className="flex-1 rounded-lg border bg-card overflow-x-auto">
                <table className="w-full min-w-max whitespace-nowrap text-sm">
                    <thead>
                    <tr className="border-b bg-muted/50">
                        <th className="text-left p-3 font-medium">Type</th>
                        <th className="text-right p-3 font-medium">Count</th>
                        <th className="text-right p-3 font-medium">Active</th>
                        <th className="text-right p-3 font-medium">Size</th>
                        <th className="text-right p-3 font-medium">Reclaimable</th>
                    </tr>
                    </thead>
                    <tbody>

                    {[1, 2, 3, 4].map((index) => (
                        <tr key={index} className="border-b last:border-b-0">
                            <td className="p-3">
                                <div className="h-4 w-24 bg-muted rounded animate-pulse"/>
                            </td>
                            <td className="p-3">
                                <div className="h-4 w-8 bg-muted rounded animate-pulse ml-auto"/>
                            </td>
                            <td className="p-3">
                                <div className="h-4 w-8 bg-muted rounded animate-pulse ml-auto"/>
                            </td>
                            <td className="p-3">
                                <div className="h-4 w-16 bg-muted rounded animate-pulse ml-auto"/>
                            </td>
                            <td className="p-3">
                                <div className="h-4 w-24 bg-muted rounded animate-pulse ml-auto"/>
                            </td>
                        </tr>
                    ))}
                    </tbody>
                </table>
            </div>

            <div className="w-1/3 shrink-0 flex items-center justify-center">
                <div className="size-30 rounded-full bg-muted animate-pulse"/>
            </div>
        </div>
    );
}

/**
 * When `nodeId` is provided, only renders if the node matches the Docker host
 * Cetacean is connected to (disk usage data is local to that host).
 */
export default function DiskUsageSection({nodeId}: { nodeId?: string }) {
    const [data, setData] = useState<DiskUsageSummary[] | null>(null);
    const [visible, setVisible] = useState(!nodeId);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        if (!nodeId) {
            return;
        }
        api
            .cluster()
            .then(({localNodeID}) => {
                if (localNodeID && localNodeID === nodeId) {
                    setVisible(true);
                } else {
                    setLoading(false);
                }
            })
            .catch(() => setLoading(false));
    }, [nodeId]);

    useEffect(() => {
        if (!visible) {
            return;
        }

        api
            .diskUsage()
            .then(setData)
            .catch(() => {
            })
            .finally(() => setLoading(false));
    }, [visible]);

    if (!visible ||
        (
            !loading && !data
        )) {
        return null;
    }

    return (
        <CollapsibleSection title="Docker Disk Usage">
            {data ? <DiskUsageTable data={data}/> : <DiskUsageLoading/>}
        </CollapsibleSection>
    );
}
