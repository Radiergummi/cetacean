import type { Task } from "../api/types";
import type { TaskMetricsData } from "../hooks/useTaskMetrics";
import CollapsibleSection from "./CollapsibleSection";
import TimeRangeControl from "./metrics/TimeRangeControl";
import { rangeSeconds } from "./metrics/useMetricsSeries";
import { useTimeRange } from "./metrics/useTimeRange";
import SegmentedControl from "./SegmentedControl";
import TasksTable from "./TasksTable";
import TaskStateFilter from "./TaskStateFilter";
import TaskTimeline from "./TaskTimeline";
import { api } from "@/api/client";
import { isInWindow, type TimelineVariant, type TimelineWindow } from "@/lib/taskTimeline";
import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

type TaskView = "list" | "timeline";

const viewSegments: { value: TaskView; label: string }[] = [
  { value: "list", label: "List" },
  { value: "timeline", label: "Timeline" },
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
 * The page's time range in epoch milliseconds, and the tasks alive inside it.
 * Only the open section's controls and body call it, so a collapsed one does
 * not tick.
 */
function useTasksInWindow(tasks: Task[]) {
  const { range, customFrom, customTo } = useTimeRange();
  const now = useNow(30_000);
  const timeWindow = useMemo<TimelineWindow>(
    () =>
      customFrom != null && customTo != null
        ? { start: customFrom * 1000, end: customTo * 1000 }
        : { start: now - rangeSeconds(range) * 1000, end: now },
    [customFrom, customTo, range, now],
  );
  const inWindow = useMemo(
    () => tasks.filter((task) => isInWindow(task, timeWindow, now)),
    [tasks, timeWindow, now],
  );

  return { timeWindow, now, inWindow };
}

function TasksControls({
  tasks,
  view,
  onViewChange,
  stateFilter,
  onStateFilterChange,
}: {
  tasks: Task[];
  view: TaskView;
  onViewChange: (view: TaskView) => void;
  stateFilter: string | null;
  onStateFilterChange: (state: string | null) => void;
}) {
  const { inWindow } = useTasksInWindow(tasks);

  return (
    <>
      {view === "list" && (
        <TaskStateFilter
          tasks={inWindow}
          active={stateFilter}
          onChange={onStateFilterChange}
        />
      )}
      <TimeRangeControl />
      <SegmentedControl
        segments={viewSegments}
        value={view}
        onChange={onViewChange}
      />
    </>
  );
}

function TasksBody({
  tasks,
  view,
  variant,
  timelineVariant,
  metrics,
  stateFilter,
  onStateFilterChange,
}: {
  tasks: Task[];
  view: TaskView;
  variant: "service" | "node";
  timelineVariant: TimelineVariant;
  metrics: Record<string, TaskMetricsData> | undefined;
  stateFilter: string | null;
  onStateFilterChange: (state: string | null) => void;
}) {
  const { timeWindow, now, inWindow } = useTasksInWindow(tasks);
  const { data: swarm } = useQuery({
    queryKey: ["swarm"],
    queryFn: () => api.swarm(),
    enabled: view === "timeline",
  });
  const retentionLimit =
    swarm?.data.swarm.Spec.Orchestration.TaskHistoryRetentionLimit ?? defaultRetentionLimit;

  if (view === "timeline") {
    return (
      <TaskTimeline
        tasks={tasks}
        variant={timelineVariant}
        timeWindow={timeWindow}
        now={now}
        retentionLimit={retentionLimit}
      />
    );
  }

  return (
    <TasksTable
      tasks={inWindow}
      variant={variant}
      metrics={metrics}
      stateFilter={stateFilter}
      onStateFilterChange={onStateFilterChange}
    />
  );
}

/**
 * A resource's tasks over the page's time range, as a list or as a timeline.
 * The view is in the URL as `taskView`, so a link can open on either.
 */
export default function TasksSection({
  tasks,
  variant,
  timelineVariant,
  metrics,
}: {
  tasks: Task[];
  variant: "service" | "node";
  timelineVariant: TimelineVariant;
  metrics?: Record<string, TaskMetricsData> | undefined;
}) {
  const [params, setParams] = useSearchParams();
  const view: TaskView = params.get("taskView") === "timeline" ? "timeline" : "list";
  const [stateFilter, setStateFilter] = useState<string | null>(null);

  const setView = useCallback(
    (next: TaskView) => {
      setParams(
        (previous) => {
          const updated = new URLSearchParams(previous);

          if (next === "timeline") {
            updated.set("taskView", next);
          } else {
            updated.delete("taskView");
          }

          return updated;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  if (tasks.length === 0) {
    return null;
  }

  return (
    <CollapsibleSection
      title="Tasks"
      controls={
        <TasksControls
          tasks={tasks}
          view={view}
          onViewChange={setView}
          stateFilter={stateFilter}
          onStateFilterChange={setStateFilter}
        />
      }
    >
      <TasksBody
        tasks={tasks}
        view={view}
        variant={variant}
        timelineVariant={timelineVariant}
        metrics={metrics}
        stateFilter={stateFilter}
        onStateFilterChange={setStateFilter}
      />
    </CollapsibleSection>
  );
}
