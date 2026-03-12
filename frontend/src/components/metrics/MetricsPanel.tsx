import type React from "react";
import { useState, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { RefreshCw, Play, Square, ChevronRight } from "lucide-react";
import TimeSeriesChart from "./TimeSeriesChart";
import type { Threshold } from "./TimeSeriesChart";
import SegmentedControl from "../SegmentedControl";
import type { Segment } from "../SegmentedControl";

interface ChartDef {
  title: string;
  query: string;
  unit?: string;
  thresholds?: Threshold[];
  yMin?: number;
  color?: string;
}

interface Props {
  charts: ChartDef[];
  header?: React.ReactNode;
}

const RANGES = ["1h", "6h", "24h", "7d"] as const;
const RANGE_SEGMENTS: Segment<string>[] = RANGES.map((r) => ({ value: r, label: r }));

export default function MetricsPanel({ charts, header }: Props) {
  const [params, setParams] = useSearchParams();
  const range = params.get("range") ?? "1h";

  const setRange = (r: string) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (r === "1h") next.delete("range");
        else next.set("range", r);
        return next;
      },
      { replace: true },
    );
  };
  const [refreshKey, setRefreshKey] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [collapsed, setCollapsed] = useState(false);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(() => setRefreshKey((k) => k + 1), 30000);
    return () => clearInterval(interval);
  }, [autoRefresh]);

  const controls = (
    <div className="flex flex-wrap items-center gap-2">
      <SegmentedControl segments={RANGE_SEGMENTS} value={range} onChange={setRange} />
      <button
        onClick={() => setRefreshKey((k) => k + 1)}
        className="h-8 w-8 flex items-center justify-center rounded-md border hover:bg-muted"
        title="Refresh"
      >
        <RefreshCw className="size-3.5" />
      </button>
      <button
        onClick={() => setAutoRefresh((v) => !v)}
        aria-pressed={autoRefresh}
        title={autoRefresh ? "Pause auto-refresh" : "Auto-refresh (30s)"}
        className="h-8 w-8 flex items-center justify-center rounded-md border hover:bg-muted aria-pressed:bg-primary aria-pressed:text-primary-foreground aria-pressed:border-primary"
      >
        {autoRefresh ? <Square className="size-3.5" /> : <Play className="size-3.5" />}
      </button>
    </div>
  );

  const toggle = (
    <button
      type="button"
      onClick={() => setCollapsed((c) => !c)}
      className="flex items-center gap-1.5 text-sm font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
    >
      <ChevronRight
        data-open={!collapsed || undefined}
        className="h-4 w-4 transition-transform data-open:rotate-90"
      />
      {header ?? "Metrics"}
    </button>
  );

  return (
    <div>
      <div className="flex items-center gap-2 mb-4 min-h-8">
        {toggle}
        {!collapsed && <div className="ml-auto">{controls}</div>}
      </div>
      {!collapsed && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {charts.map((chart) => (
            <TimeSeriesChart
              key={chart.query}
              {...chart}
              range={range}
              refreshKey={refreshKey}
              syncKey="metrics"
            />
          ))}
        </div>
      )}
    </div>
  );
}
