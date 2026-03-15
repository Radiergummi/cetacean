import { useState, useCallback } from "react";
import { ChevronLeft } from "lucide-react";
import TimeSeriesChart from "./TimeSeriesChart";
import type { Threshold } from "./TimeSeriesChart";
import { useMetricsPanelContext } from "./MetricsPanel";

interface Props {
  title: string;
  stackQuery: string;
  serviceQueryTemplate: string;
  unit?: string;
  yMin?: number;
  range?: string;
  from?: number;
  to?: number;
  refreshKey?: number;
  syncKey?: string;
  onRangeSelect?: (from: number, to: number) => void;
  thresholds?: Threshold[];
  stackable?: boolean;
}

export default function StackDrillDownChart({
  title,
  stackQuery,
  serviceQueryTemplate,
  unit,
  yMin,
  range,
  from,
  to,
  refreshKey,
  syncKey,
  onRangeSelect,
  thresholds,
  stackable,
}: Props) {
  const panel = useMetricsPanelContext();
  const effectiveRange = range ?? panel?.range ?? "1h";
  const effectiveFrom = from ?? panel?.from;
  const effectiveTo = to ?? panel?.to;
  const effectiveRefreshKey = refreshKey ?? panel?.refreshKey;
  const effectiveSyncKey = syncKey;
  const effectiveOnRangeSelect = onRangeSelect ?? panel?.onRangeSelect;

  const [drillStack, setDrillStack] = useState<string | null>(null);
  const [showLegend, setShowLegend] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const [seriesInfo, setSeriesInfo] = useState<{ label: string; color: string }[]>([]);

  const handleSeriesDoubleClick = useCallback((seriesLabel: string) => {
    setDrillStack(seriesLabel);
  }, []);

  const handleBack = useCallback(() => {
    setDrillStack(null);
  }, []);

  const effectiveStackQuery = showAll
    ? stackQuery.replace("topk(10,", "topk(30,")
    : stackQuery;

  const query = drillStack
    ? serviceQueryTemplate.replace("<STACK>", drillStack)
    : effectiveStackQuery;

  const chartTitle = drillStack ? `${title} (${drillStack})` : title;

  return (
    <div>
      {drillStack && (
        <button
          onClick={handleBack}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-1"
        >
          <ChevronLeft className="size-3" />
          All Stacks
        </button>
      )}
      <TimeSeriesChart
        title={chartTitle}
        query={query}
        unit={unit}
        yMin={yMin}
        range={effectiveRange}
        from={effectiveFrom}
        to={effectiveTo}
        refreshKey={effectiveRefreshKey}
        syncKey={effectiveSyncKey}
        onRangeSelect={effectiveOnRangeSelect}
        thresholds={thresholds}
        onSeriesDoubleClick={drillStack ? undefined : handleSeriesDoubleClick}
        stackable={stackable}
        onSeriesInfo={setSeriesInfo}
      />
      <div className="mt-1">
        <button
          onClick={() => setShowLegend((v) => !v)}
          className="text-[11px] text-muted-foreground hover:text-foreground"
        >
          {showLegend ? "Hide legend" : "Show legend"}
        </button>
        {showLegend && seriesInfo.length > 0 && (
          <div className="flex flex-wrap gap-x-3 gap-y-1 mt-1">
            {seriesInfo.map((s, i) => (
              <span
                key={s.label}
                className="flex items-center gap-1.5 text-[11px] text-muted-foreground"
                style={{ opacity: i >= 10 && !showAll ? 0.3 : 1 }}
              >
                <span className="w-2 h-2 rounded-full shrink-0" style={{ background: s.color }} />
                {s.label}
              </span>
            ))}
            {!drillStack && (
              <button
                onClick={() => setShowAll((v) => !v)}
                className="text-[11px] text-primary hover:underline"
              >
                {showAll ? "Top 10 only" : "Show all"}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
