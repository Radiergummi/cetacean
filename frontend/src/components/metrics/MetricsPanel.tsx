import type React from "react";
import { useState, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { RefreshCw, Play, Square } from "lucide-react";
import TimeSeriesChart from "./TimeSeriesChart";
import { ChartSyncProvider } from "./ChartSyncProvider";
import type { Threshold } from "./TimeSeriesChart";
import CollapsibleSection from "../CollapsibleSection";
import { IconButton } from "../IconButton";
import SegmentedControl from "../SegmentedControl";
import type { Segment } from "../SegmentedControl";
import RangePicker from "./RangePicker";

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

  const fromParam = params.get("from");
  const toParam = params.get("to");
  const customFrom = fromParam ? Number(fromParam) : null;
  const customTo = toParam ? Number(toParam) : null;
  const isCustomRange = customFrom != null && customTo != null;

  const setCustomRange = (from: number, to: number) => {
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set("from", String(Math.floor(from)));
      next.set("to", String(Math.floor(to)));
      next.delete("range");
      return next;
    }, { replace: true });
  };

  const clearCustomRange = () => {
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete("from");
      next.delete("to");
      return next;
    }, { replace: true });
  };
  const [refreshKey, setRefreshKey] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(false);

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(() => setRefreshKey((k) => k + 1), 30000);
    return () => clearInterval(interval);
  }, [autoRefresh]);

  const controls = (
    <div className="flex flex-wrap items-center gap-2">
      <SegmentedControl segments={RANGE_SEGMENTS} value={isCustomRange ? "" : range} onChange={setRange} />
      <RangePicker from={customFrom} to={customTo} onApply={setCustomRange} onClear={clearCustomRange} />
      <IconButton
        onClick={() => setRefreshKey((k) => k + 1)}
        title="Refresh"
        icon={<RefreshCw className="size-3.5" />}
      />
      <IconButton
        onClick={() => setAutoRefresh((v) => !v)}
        title={autoRefresh ? "Pause auto-refresh" : "Auto-refresh (30s)"}
        icon={autoRefresh ? <Square className="size-3.5" /> : <Play className="size-3.5" />}
        active={autoRefresh}
      />
    </div>
  );

  return (
    <CollapsibleSection title={typeof header === "string" ? header : "Metrics"} controls={controls}>
      <ChartSyncProvider syncKey="metrics">
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {charts.map((chart) => (
            <TimeSeriesChart
              key={chart.query}
              {...chart}
              range={range}
              from={customFrom ?? undefined}
              to={customTo ?? undefined}
              refreshKey={refreshKey}
              syncKey="metrics"
              onRangeSelect={setCustomRange}
            />
          ))}
        </div>
      </ChartSyncProvider>
    </CollapsibleSection>
  );
}
