import type { Task } from "../api/types";
import type { TaskMetricsData } from "../hooks/useTaskMetrics";
import { useUrlParam } from "../hooks/useUrlParam";
import { SectionToggle, useSectionCollapse } from "./CollapsibleSection";
import TimeRangeControl from "./metrics/TimeRangeControl";
import { rangeSeconds } from "./metrics/useMetricsSeries";
import { useTimeRange } from "./metrics/useTimeRange";
import SegmentedControl, { type Segment } from "./SegmentedControl";
import TasksTable from "./TasksTable";
import TaskStateFilter from "./TaskStateFilter";
import TaskTimeline from "./TaskTimeline";
import { api } from "@/api/client";
import { isInWindow, type TimelineVariant, type TimelineWindow } from "@/lib/taskTimeline";
import { useQuery } from "@tanstack/react-query";
import { ChartGantt, List } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";

type TaskView = "list" | "timeline";

const viewSegments: Segment<TaskView>[] = [
  { value: "list", label: "List", icon: <List className="size-3.5" /> },
  { value: "timeline", label: "Timeline", icon: <ChartGantt className="size-3.5" /> },
];

/**
 * Swarm's own default, used while the swarm's setting is unknown or unset.
 */
const defaultRetentionLimit = 5;

/**
 * Skips ticks while the tab is hidden and catches up when it is shown again.
 */
function useNow(intervalMilliseconds: number): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const tick = () => {
      if (document.visibilityState === "visible") {
        setNow(Date.now());
      }
    };
    const id = setInterval(tick, intervalMilliseconds);

    document.addEventListener("visibilitychange", tick);

    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", tick);
    };
  }, [intervalMilliseconds]);

  return now;
}

/**
 * The open section: it holds the clock, so a collapsed one does not tick.
 */
function OpenTasks({
  tasks,
  variant,
  metrics,
  view,
  onViewChange,
  header,
}: {
  tasks: Task[];
  variant: TimelineVariant;
  metrics: Record<string, TaskMetricsData> | undefined;
  view: TaskView;
  onViewChange: (view: string) => void;
  header: (controls: ReactNode) => ReactNode;
}) {
  const { range, customFrom, customTo } = useTimeRange();
  const now = useNow(30_000);
  const [stateFilter, setStateFilter] = useState<string | null>(null);
  const timeWindow = useMemo<TimelineWindow>(
    () =>
      customFrom != null && customTo != null
        ? { start: customFrom * 1000, end: customTo * 1000 }
        : { start: now - rangeSeconds(range) * 1000, end: now },
    [customFrom, customTo, range, now],
  );
  const inWindow = useMemo(
    () => (view === "list" ? tasks.filter((task) => isInWindow(task, timeWindow, now)) : tasks),
    [view, tasks, timeWindow, now],
  );
  const { data: swarm } = useQuery({
    queryKey: ["swarm"],
    queryFn: () => api.swarm(),
    enabled: view === "timeline",
    staleTime: Number.POSITIVE_INFINITY,
  });

  return (
    <>
      {header(
        <>
          {view === "list" && (
            <TaskStateFilter
              tasks={inWindow}
              active={stateFilter}
              onChange={setStateFilter}
            />
          )}
          <TimeRangeControl />
          <SegmentedControl
            segments={viewSegments}
            value={view}
            onChange={onViewChange}
          />
        </>,
      )}

      {view === "timeline" ? (
        <TaskTimeline
          tasks={tasks}
          variant={variant}
          timeWindow={timeWindow}
          now={now}
          retentionLimit={
            swarm?.data.swarm.Spec.Orchestration.TaskHistoryRetentionLimit ?? defaultRetentionLimit
          }
        />
      ) : (
        <TasksTable
          tasks={inWindow}
          variant={variant === "node" ? "node" : "service"}
          metrics={metrics}
          stateFilter={stateFilter}
          onStateFilterChange={setStateFilter}
        />
      )}
    </>
  );
}

/**
 * A resource's tasks over the page's time range, as a list or as a timeline.
 * The view is in the URL as `taskView`, so a link can open on either.
 */
export default function TasksSection({
  tasks,
  variant,
  metrics,
}: {
  tasks: Task[];
  variant: TimelineVariant;
  metrics?: Record<string, TaskMetricsData> | undefined;
}) {
  const { open, toggle } = useSectionCollapse("Tasks");
  const [viewParam, setView] = useUrlParam("taskView", "list");
  const view: TaskView = viewParam === "timeline" ? "timeline" : "list";

  if (tasks.length === 0) {
    return null;
  }

  const header = (controls: ReactNode) => (
    <div className="mb-3 flex min-h-8 flex-wrap items-center gap-2">
      <h2>
        <SectionToggle
          title="Tasks"
          open={open}
          onToggle={toggle}
        />
      </h2>
      {controls && <div className="flex items-center gap-2 sm:ms-auto">{controls}</div>}
    </div>
  );

  return (
    <div>
      {open ? (
        <OpenTasks
          tasks={tasks}
          variant={variant}
          metrics={metrics}
          view={view}
          onViewChange={setView}
          header={header}
        />
      ) : (
        header(null)
      )}
    </div>
  );
}
