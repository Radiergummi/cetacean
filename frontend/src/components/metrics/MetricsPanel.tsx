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

  useEffect(() => {
    if (!autoRefresh) return;
    const interval = setInterval(() => setRefreshKey((k) => k + 1), 30000);
    return () => clearInterval(interval);
  }, [autoRefresh]);

  const controls = (
    <div className="flex flex-wrap items-center gap-2">
      <SegmentedControl segments={RANGE_SEGMENTS} value={range} onChange={setRange} />
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
              refreshKey={refreshKey}
              syncKey="metrics"
            />
          ))}
        </div>
      </ChartSyncProvider>
    </CollapsibleSection>
  );
}
