import CollapsibleSection from "../CollapsibleSection";
import { IconButton } from "../IconButton";
import type { Segment } from "../SegmentedControl";
import SegmentedControl from "../SegmentedControl";
import { ChartSyncProvider } from "./ChartSyncProvider";
import { MetricsPanelContext, type MetricsPanelContextValue } from "./MetricsPanelContext";
import type { Threshold } from "./TimeSeriesChart";
import TimeSeriesChart from "./TimeSeriesChart";
import { AreaChart, Calendar, LineChart, Pause, Play, RefreshCw, X } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

function formatRange(from: number, to: number): string {
  const f = new Date(from * 1000);
  const t = new Date(to * 1000);
  const sameDay = f.toDateString() === t.toDateString();
  const dateFmt: Intl.DateTimeFormatOptions = { month: "short", day: "numeric" };
  const timeFmt: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit" };
  if (sameDay) {
    return `${f.toLocaleDateString(undefined, dateFmt)} ${f.toLocaleTimeString(
      undefined,
      timeFmt,
    )} – ${t.toLocaleTimeString(undefined, timeFmt)}`;
  }
  return `${f.toLocaleDateString(undefined, dateFmt)} ${f.toLocaleTimeString(
    undefined,
    timeFmt,
  )} – ${t.toLocaleDateString(undefined, dateFmt)} ${t.toLocaleTimeString(undefined, timeFmt)}`;
}

const QUICK_PRESETS = [
  { label: "Last 2h", seconds: 7200 },
  { label: "Last 12h", seconds: 43200 },
  { label: "Last 48h", seconds: 172800 },
  { label: "Last 3d", seconds: 259200 },
];

export { useMetricsPanelContext } from "./MetricsPanelContext";
export type { MetricsPanelContextValue } from "./MetricsPanelContext";

interface ChartDef {
  title: string;
  query: string;
  unit?: string;
  thresholds?: Threshold[];
  yMin?: number;
  color?: string;
}

interface Props {
  charts?: ChartDef[];
  children?: React.ReactNode;
  header?: React.ReactNode;
  stackable?: boolean;
}

const ranges = ["1h", "6h", "24h", "7d"] as const;
const rangeSegments: Segment<string>[] = ranges.map((value) => ({
  label: value.toLocaleUpperCase(),
  value,
}));

export default function MetricsPanel({ charts, children, header, stackable }: Props) {
  const [params, setParams] = useSearchParams();
  const range = params.get("range") ?? "1h";

  const setRange = (r: string) => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (r === "1h") {
          next.delete("range");
        } else {
          next.set("range", r);
        }
        next.delete("from");
        next.delete("to");
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

  const setCustomRange = useCallback(
    (from: number, to: number) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("from", String(Math.floor(from)));
          next.set("to", String(Math.floor(to)));
          next.delete("range");
          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  const clearCustomRange = useCallback(() => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("from");
        next.delete("to");
        return next;
      },
      { replace: true },
    );
  }, [setParams]);
  const [refreshKey, setRefreshKey] = useState(0);
  const [stacked, setStacked] = useState(false);
  const [streaming, setStreaming] = useState(true);
  const [startInput, setStartInput] = useState("");
  const [endInput, setEndInput] = useState("");
  const [drillStack, setDrillStack] = useState<string | null>(null);

  useEffect(() => {
    if (!drillStack) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") setDrillStack(null);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [drillStack]);

  const panelCtx = useMemo<MetricsPanelContextValue>(
    () => ({
      range,
      from: customFrom ?? undefined,
      to: customTo ?? undefined,
      refreshKey,
      onRangeSelect: setCustomRange,
      stacked,
      streaming,
      drillStack,
      setDrillStack,
    }),
    [range, customFrom, customTo, refreshKey, setCustomRange, stacked, streaming, drillStack],
  );

  const handleCustomApply = (close: () => void) => {
    const s = new Date(startInput).getTime() / 1000;
    const e = new Date(endInput).getTime() / 1000;
    if (!isNaN(s) && !isNaN(e) && s < e) {
      setCustomRange(s, e);
      close();
    }
  };

  const handlePreset = (seconds: number, close: () => void) => {
    const now = Math.floor(Date.now() / 1000);
    setCustomRange(now - seconds, now);
    close();
  };

  const customRangeLabel = isCustomRange ? formatRange(customFrom!, customTo!) : undefined;

  const controls = (
    <div className="flex flex-wrap items-center gap-2">
      {drillStack && (
        <button
          onClick={() => setDrillStack(null)}
          className="flex items-center gap-1 rounded-md border border-primary/30 bg-primary/10 px-2 py-1.5 text-xs text-primary hover:bg-primary/20"
          title="Clear stack filter (Esc)"
        >
          <span className="font-medium">{drillStack}</span>
          <X className="size-3" />
        </button>
      )}
      {stackable && (
        <IconButton
          onClick={() => setStacked((v) => !v)}
          title={stacked ? "Switch to line chart" : "Switch to stacked area"}
          icon={stacked ? <AreaChart className="size-3.5" /> : <LineChart className="size-3.5" />}
        />
      )}
      <SegmentedControl
        segments={rangeSegments}
        value={isCustomRange ? ("" as string) : range}
        onChange={(v) => setRange(v)}
        overflowIcon={<Calendar className="size-3" />}
        overflowActive={isCustomRange}
        overflowLabel={
          isCustomRange ? (
            <span className="flex items-center gap-1 py-0.5 text-xs">
              {customRangeLabel}
              <X
                className="size-3 opacity-60 hover:opacity-100"
                onClick={(event) => {
                  event.stopPropagation();
                  clearCustomRange();
                }}
              />
            </span>
          ) : undefined
        }
        overflowContent={(close) => (
          <div className="w-64 p-2">
            <div className="mb-3 grid grid-cols-2 gap-1.5">
              {QUICK_PRESETS.map(({ label, seconds }) => (
                <button
                  key={label}
                  onClick={() => handlePreset(seconds, close)}
                  className="rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                >
                  {label}
                </button>
              ))}
            </div>
            <div className="flex flex-col gap-2">
              <label className="block">
                <span className="text-xs text-muted-foreground">From</span>
                <input
                  type="datetime-local"
                  value={startInput}
                  onChange={(e) => setStartInput(e.target.value)}
                  className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
                />
              </label>
              <label className="block">
                <span className="text-xs text-muted-foreground">To</span>
                <input
                  type="datetime-local"
                  value={endInput}
                  onChange={(e) => setEndInput(e.target.value)}
                  className="mt-0.5 w-full rounded-md border bg-card px-2 py-1 text-xs"
                />
              </label>
              <button
                onClick={() => handleCustomApply(close)}
                className="w-full rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
              >
                Apply
              </button>
            </div>
          </div>
        )}
      />
      <IconButton
        onClick={() => setRefreshKey((k) => k + 1)}
        title="Refresh"
        icon={<RefreshCw className="size-3.5" />}
      />
      <IconButton
        onClick={() => setStreaming((v) => !v)}
        title={streaming ? "Pause live streaming" : "Resume live streaming"}
        icon={streaming ? <Pause className="size-3.5" /> : <Play className="size-3.5" />}
        active={streaming}
      />
    </div>
  );

  return (
    <CollapsibleSection
      title={typeof header === "string" ? header : "Metrics"}
      controls={controls}
    >
      <MetricsPanelContext.Provider value={panelCtx}>
        <ChartSyncProvider>
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {children ??
              charts?.map((chart) => (
                <TimeSeriesChart
                  key={chart.query}
                  {...chart}
                  range={range}
                  from={customFrom ?? undefined}
                  to={customTo ?? undefined}
                  refreshKey={refreshKey}
                  onRangeSelect={setCustomRange}
                />
              ))}
          </div>
        </ChartSyncProvider>
      </MetricsPanelContext.Provider>
    </CollapsibleSection>
  );
}
