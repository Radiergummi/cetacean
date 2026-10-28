import type { Task } from "@/api/types";
import CollapsibleSection from "@/components/CollapsibleSection";
import { rangeSegments } from "@/components/metrics/MetricsPanelContext";
import { rangeSeconds } from "@/components/metrics/useMetricsSeries";
import { useTimeRange } from "@/components/metrics/useTimeRange";
import SegmentedControl from "@/components/SegmentedControl";
import TaskStatusBadge from "@/components/TaskStatusBadge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDateTime, formatDuration } from "@/lib/format";
import { statusColor } from "@/lib/statusColor";
import {
  buildTimeline,
  type TimelineBar,
  type TimelineVariant,
  type TimelineWindow,
} from "@/lib/taskTimeline";
import { cn } from "@/lib/utils";
import { X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

const labelWidth = "w-24 shrink-0 sm:w-40";
const day = 86_400_000;

function useNow(intervalMilliseconds: number): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMilliseconds);

    return () => clearInterval(id);
  }, [intervalMilliseconds]);

  return now;
}

function formatAxisTime(time: number, timeWindow: TimelineWindow): string {
  return formatDateTime(
    new Date(time),
    timeWindow.end - timeWindow.start > day
      ? { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }
      : { hour: "2-digit", minute: "2-digit" },
  );
}

function percentOf(time: number, { start, end }: TimelineWindow): number {
  return Math.min(100, Math.max(0, ((time - start) / (end - start)) * 100));
}

function Bar({
  bar,
  lanes,
  variant,
  timeWindow,
}: {
  bar: TimelineBar;
  lanes: number;
  variant: TimelineVariant;
  timeWindow: TimelineWindow;
}) {
  const { task, start, end, lane, ongoing, failed, error } = bar;
  const left = percentOf(start, timeWindow);
  const width = percentOf(end, timeWindow) - left;
  const context =
    variant !== "node"
      ? task.NodeHostname || task.NodeID?.slice(0, 12)
      : task.ServiceName || task.ServiceID.slice(0, 12);

  return (
    <Tooltip>
      <TooltipTrigger
        render={<Link to={`/tasks/${task.ID}`} />}
        aria-label={`Task ${task.ID.slice(0, 12)}, ${task.Status.State}`}
        className={cn(
          "absolute min-w-0.5 rounded-sm outline-none hover:brightness-110 focus-visible:ring-2 focus-visible:ring-ring",
          statusColor(task.Status.State),
          ongoing && "rounded-r-none",
        )}
        style={{
          left: `${left}%`,
          width: `${width}%`,
          top: `calc(${(lane / lanes) * 100}% + 2px)`,
          height: `calc(${100 / lanes}% - 4px)`,
        }}
      >
        {failed && (
          <span className="absolute -inset-y-0.5 -right-px w-0.5 rounded-full bg-status-danger" />
        )}
      </TooltipTrigger>
      <TooltipContent
        side="bottom"
        className="flex flex-col gap-1"
      >
        <span className="flex items-center gap-2">
          <span className="font-mono">{task.ID.slice(0, 12)}</span>
          <TaskStatusBadge state={task.Status.State} />
        </span>
        {context && <span className="text-muted-foreground">{context}</span>}
        <span>
          {formatDateTime(new Date(start))} – {ongoing ? "now" : formatDateTime(new Date(end))}
          {" · "}
          {formatDuration((end - start) * 1_000_000)}
        </span>
        {error && <span className="text-status-danger">{error}</span>}
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * Task lifetimes as bars over the same URL time range as the metric charts.
 */
export default function TaskTimeline({
  tasks,
  variant,
}: {
  tasks: Task[];
  variant: TimelineVariant;
}) {
  const { range, customFrom, customTo, setRange, clearCustomRange } = useTimeRange();
  const isCustomRange = customFrom != null && customTo != null;
  const now = useNow(30_000);
  const timeWindow = useMemo<TimelineWindow>(
    () =>
      isCustomRange
        ? { start: customFrom * 1000, end: customTo * 1000 }
        : { start: now - rangeSeconds(range) * 1000, end: now },
    [isCustomRange, customFrom, customTo, range, now],
  );
  const rows = useMemo(
    () => buildTimeline(tasks, variant, timeWindow, now),
    [tasks, variant, timeWindow, now],
  );

  if (tasks.length === 0) {
    return null;
  }

  const controls = (
    <>
      {isCustomRange && (
        <button
          type="button"
          onClick={clearCustomRange}
          className="flex items-center gap-1 rounded-md border px-2 py-1.5 text-xs text-muted-foreground hover:text-foreground"
          title="Clear custom range"
        >
          Custom range
          <X className="size-3" />
        </button>
      )}
      <SegmentedControl
        segments={rangeSegments}
        value={isCustomRange ? "" : range}
        onChange={setRange}
      />
    </>
  );

  return (
    <CollapsibleSection
      title="Task Timeline"
      controls={controls}
    >
      <div className="flex flex-col gap-1 rounded-lg border bg-card p-3">
        {rows.length === 0 && (
          <p className="py-2 text-center text-sm text-muted-foreground">
            No tasks in this time range
          </p>
        )}

        {rows.map(({ key, label, bars, lanes, earliest }) => {
          const unretained = earliest > timeWindow.start ? percentOf(earliest, timeWindow) : 0;

          return (
            <div
              key={key}
              className="flex items-center gap-2"
            >
              <span
                className={cn(labelWidth, "truncate text-xs text-muted-foreground")}
                title={label}
              >
                {label}
              </span>
              <div
                className="relative flex-1 rounded-sm bg-muted/50"
                style={{ height: `${lanes * 1.25}rem` }}
                title={
                  unretained > 0
                    ? `No task history before ${formatAxisTime(earliest, timeWindow)}`
                    : undefined
                }
              >
                {unretained > 0 && (
                  <div
                    className="absolute inset-y-0 left-0 rounded-l-sm bg-[repeating-linear-gradient(135deg,transparent_0_4px,var(--color-border)_4px_5px)]"
                    style={{ width: `${unretained}%` }}
                  />
                )}
                {bars.map((bar) => (
                  <Bar
                    key={bar.task.ID}
                    bar={bar}
                    lanes={lanes}
                    variant={variant}
                    timeWindow={timeWindow}
                  />
                ))}
              </div>
            </div>
          );
        })}

        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className={labelWidth} />
          <div className="flex flex-1 justify-between gap-2">
            <time dateTime={new Date(timeWindow.start).toISOString()}>
              {formatAxisTime(timeWindow.start, timeWindow)}
            </time>
            <time dateTime={new Date(timeWindow.end).toISOString()}>
              {isCustomRange ? formatAxisTime(timeWindow.end, timeWindow) : "now"}
            </time>
          </div>
        </div>

        {rows.some(({ earliest }) => earliest > timeWindow.start) && (
          <p className="text-xs text-muted-foreground">
            Hatched: Swarm keeps no older task history for that row.
          </p>
        )}
      </div>
    </CollapsibleSection>
  );
}
