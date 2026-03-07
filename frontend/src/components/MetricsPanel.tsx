import { useState, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { RefreshCw, Play, Square, ChevronDown, ChevronRight } from "lucide-react";
import TimeSeriesChart from "./TimeSeriesChart";
import type { Threshold } from "./TimeSeriesChart";

interface ChartDef {
  title: string;
  query: string;
  unit?: string;
  thresholds?: Threshold[];
}

interface Props {
  charts: ChartDef[];
}

const RANGES = ["1h", "6h", "24h", "7d"] as const;

export default function MetricsPanel({ charts }: Props) {
  const [params, setParams] = useSearchParams();
  const [range, setRangeState] = useState<string>(params.get("range") ?? "1h");
  const setRange = (r: string) => {
    setRangeState(r);
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

  return (
    <div>
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <button
          onClick={() => setCollapsed((c) => !c)}
          className="flex items-center gap-1 text-sm font-medium hover:text-foreground text-muted-foreground"
        >
          {collapsed ? <ChevronRight className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
          Metrics
        </button>
        {!collapsed && (
          <>
            <span className="text-sm text-muted-foreground">Time range:</span>
            {RANGES.map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={`px-3 py-1 text-sm rounded ${
                  range === r ? "bg-primary text-primary-foreground" : "bg-muted"
                }`}
              >
                {r}
              </button>
            ))}
            <button
              onClick={() => setRefreshKey((k) => k + 1)}
              className="h-8 w-8 flex items-center justify-center rounded-md border hover:bg-muted"
              title="Refresh"
            >
              <RefreshCw className="w-3.5 h-3.5" />
            </button>
            <button
              onClick={() => setAutoRefresh((v) => !v)}
              title={autoRefresh ? "Pause auto-refresh" : "Auto-refresh (30s)"}
              className={`h-8 w-8 flex items-center justify-center rounded-md border ${
                autoRefresh ? "bg-primary text-primary-foreground border-primary" : "hover:bg-muted"
              }`}
            >
              {autoRefresh ? <Square className="w-3.5 h-3.5" /> : <Play className="w-3.5 h-3.5" />}
            </button>
          </>
        )}
      </div>
      {!collapsed && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {charts.map((chart) => (
            <TimeSeriesChart key={chart.query} {...chart} range={range} refreshKey={refreshKey} />
          ))}
        </div>
      )}
    </div>
  );
}
