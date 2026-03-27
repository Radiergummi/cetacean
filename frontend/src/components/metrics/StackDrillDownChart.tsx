import { splitStackPrefix } from "@/lib/searchConstants";
import { useMetricsPanelContext } from "./MetricsPanelContext";
import type { Threshold } from "./TimeSeriesChart";
import TimeSeriesChart from "./TimeSeriesChart";
import { useCallback, useMemo, useRef, useState } from "react";

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
  onRangeSelect,
  thresholds,
  stackable,
}: Props) {
  const panel = useMetricsPanelContext();
  const effectiveRange = range ?? panel?.range ?? "1h";
  const effectiveFrom = from ?? panel?.from;
  const effectiveTo = to ?? panel?.to;
  const effectiveRefreshKey = refreshKey ?? panel?.refreshKey;
  const effectiveOnRangeSelect = onRangeSelect ?? panel?.onRangeSelect;

  const drillStack = panel?.drillStack ?? null;
  const setDrillStack = panel?.setDrillStack;

  const [showLegend, setShowLegend] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const [seriesInfo, setSeriesInfo] = useState<{ label: string; color: string }[]>([]);
  const [isolatedLabel, setIsolatedLabel] = useState<string | null>(null);

  const handleSeriesDoubleClick = useCallback(
    (seriesLabel: string) => {
      setDrillStack?.(drillStack === seriesLabel ? null : seriesLabel);
    },
    [drillStack, setDrillStack],
  );

  const effectiveStackQuery = showAll ? stackQuery.replace("topk(10,", "topk(30,") : stackQuery;

  const query = drillStack
    ? serviceQueryTemplate.replace("<STACK>", drillStack)
    : effectiveStackQuery;

  // Reset isolation when query changes (drill-down or show-all toggle)
  const prevQueryRef = useRef(query);
  if (prevQueryRef.current !== query) {
    prevQueryRef.current = query;
    if (isolatedLabel != null) {
      setIsolatedLabel(null);
    }
  }

  const chartTitle = drillStack ? `${title} (${drillStack})` : title;

  const stripStackPrefix = useMemo(
    () => (drillStack ? (label: string) => splitStackPrefix(label).name : undefined),
    [drillStack],
  );

  return (
    <div>
      <TimeSeriesChart
        title={chartTitle}
        query={query}
        unit={unit}
        yMin={yMin}
        range={effectiveRange}
        from={effectiveFrom}
        to={effectiveTo}
        refreshKey={effectiveRefreshKey}
        onRangeSelect={effectiveOnRangeSelect}
        thresholds={thresholds}
        onSeriesDoubleClick={handleSeriesDoubleClick}
        stackable={stackable}
        onSeriesInfo={setSeriesInfo}
        isolatedLabel={isolatedLabel}
        onIsolationChange={setIsolatedLabel}
        labelTransform={stripStackPrefix}
      />
      <div className="mt-1">
        <button
          onClick={() => setShowLegend((v) => !v)}
          className="text-[11px] text-muted-foreground hover:text-foreground"
        >
          {showLegend ? "Hide legend" : "Show legend"}
        </button>

        {showLegend && seriesInfo.length > 0 && (
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1">
            {seriesInfo.map(({ color, label }, index) => {
              const displayLabel = drillStack
                ? splitStackPrefix(label).name
                : label;
              const dimmed = isolatedLabel != null && isolatedLabel !== label;
              const faded = index >= 10 && !showAll;

              return (
                <button
                  key={label}
                  onClick={() => setIsolatedLabel(isolatedLabel === label ? null : label)}
                  className="flex items-center gap-1.5 text-[11px] text-muted-foreground hover:text-foreground"
                  style={{ opacity: dimmed || faded ? 0.3 : 1 }}
                >
                  <span
                    className="h-2 w-2 shrink-0 rounded-full"
                    style={{ background: color }}
                  />
                  {displayLabel}
                </button>
              );
            })}

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
