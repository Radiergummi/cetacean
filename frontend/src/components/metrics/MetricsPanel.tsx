import CollapsibleSection from "../CollapsibleSection";
import { IconButton } from "../IconButton";
import { ChartSyncProvider } from "./ChartSyncProvider";
import { MetricsPanelContext, type MetricsPanelContextValue } from "./MetricsPanelContext";
import TimeRangeControl from "./TimeRangeControl";
import type { Threshold } from "./TimeSeriesChart";
import TimeSeriesChart from "./TimeSeriesChart";
import { type TimelineSource, useTimeline } from "./useTimeline";
import { useTimeRange } from "./useTimeRange";
import type { AnnotationScope } from "@/api/types";
import { useEscapeCancel } from "@/hooks/useEscapeCancel";
import { AreaChart, LineChart, MessageSquarePlus, Pause, Play, RefreshCw, X } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useState } from "react";

interface ChartDef {
  title: string;
  query: string;
  unit?: string | undefined;
  thresholds?: Threshold[] | undefined;
  yMin?: number | undefined;
  color?: string | undefined;
  labelTransform?: ((label: string) => string) | undefined;
}

interface Props {
  charts?: ChartDef[] | undefined;
  children?: React.ReactNode | undefined;
  header?: React.ReactNode | undefined;
  stackable?: boolean | undefined;
  /**
   * The resource whose changes and gaps the charts mark.
   */
  timeline?: TimelineSource | undefined;

  /**
   * The resource a note marked on these charts is about; set only when the
   * caller may annotate it and the charts draw its timeline.
   */
  annotate?: Exclude<AnnotationScope, "cluster"> | undefined;
}

export default function MetricsPanel({
  charts,
  children,
  header,
  stackable,
  timeline,
  annotate,
}: Props) {
  const { range, customFrom, customTo, setCustomRange } = useTimeRange();
  const [refreshKey, setRefreshKey] = useState(0);
  const [stacked, setStacked] = useState(false);
  const [streaming, setStreaming] = useState(true);
  const [drillStack, setDrillStack] = useState<string | null>(null);
  const [annotating, setAnnotating] = useState(false);
  const [modifierHeld, setModifierHeld] = useState(false);
  const [openMarks, setOpenMarks] = useState(0);
  const onMarkOpen = useCallback(
    (open: boolean) => setOpenMarks((count) => count + (open ? 1 : -1)),
    [],
  );

  // Holding Ctrl or Cmd annotates for as long as it is held, as in Grafana.
  useEffect(() => {
    if (!annotate) {
      return;
    }

    const isModifier = ({ key }: KeyboardEvent) => key === "Control" || key === "Meta";
    const down = (event: KeyboardEvent) => isModifier(event) && setModifierHeld(true);
    const up = (event: KeyboardEvent) => isModifier(event) && setModifierHeld(false);
    const release = () => setModifierHeld(false);

    document.addEventListener("keydown", down);
    document.addEventListener("keyup", up);
    window.addEventListener("blur", release);

    return () => {
      document.removeEventListener("keydown", down);
      document.removeEventListener("keyup", up);
      window.removeEventListener("blur", release);
    };
  }, [annotate]);
  const markers = useTimeline(timeline, {
    range,
    from: customFrom ?? undefined,
    to: customTo ?? undefined,
    refreshKey,
  });

  useEffect(() => {
    if (!drillStack) {
      return;
    }

    const handler = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setDrillStack(null);
      }
    };

    document.addEventListener("keydown", handler);

    return () => document.removeEventListener("keydown", handler);
  }, [drillStack]);

  useEscapeCancel(annotating, () => setAnnotating(false));

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
      timeline: markers.items,
      annotate,
      annotating: (annotating || modifierHeld) && annotate !== undefined,
      setAnnotating,
      markOpen: openMarks > 0,
      onMarkOpen,
    }),
    [
      range,
      customFrom,
      customTo,
      refreshKey,
      setCustomRange,
      stacked,
      streaming,
      drillStack,
      markers.items,
      annotate,
      annotating,
      modifierHeld,
      openMarks,
      onMarkOpen,
    ],
  );

  const controls = (
    <div className="flex flex-wrap items-center gap-2">
      {drillStack && (
        <button
          type="button"
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

      {annotate && (
        <IconButton
          onClick={() => setAnnotating((value) => !value)}
          title="Annotate"
          icon={<MessageSquarePlus className="size-3.5" />}
          active={annotating}
        />
      )}

      <TimeRangeControl />
      <IconButton
        onClick={() => setRefreshKey((key) => key + 1)}
        title="Refresh"
        icon={<RefreshCw className="size-3.5" />}
      />
      <IconButton
        onClick={() => setStreaming((value) => !value)}
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
          {markers.truncated && (
            <p className="mt-2 text-xs text-muted-foreground">
              Markers show the newest changes only; narrow the range to see all of them.
            </p>
          )}
        </ChartSyncProvider>
      </MetricsPanelContext.Provider>
    </CollapsibleSection>
  );
}
