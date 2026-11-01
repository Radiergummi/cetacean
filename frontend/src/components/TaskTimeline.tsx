import type { Task } from "@/api/types";
import { useTimeRange } from "@/components/metrics/useTimeRange";
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
import { memo, useMemo, useRef, useState } from "react";
import type React from "react";
import { Link } from "react-router-dom";

const labelWidth = "w-24 shrink-0 sm:w-40";
const day = 86_400_000;

/**
 * How far the pointer has to travel before a press on the tracks is a drag
 * rather than a click on a bar.
 */
const dragThreshold = 4;

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

// Memoised: a drag re-renders the timeline at pointer rate, and the bars
// do not change while it lasts.
const Bar = memo(function Bar({
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
  const shortID = task.ID.slice(0, 12);
  const context =
    variant !== "node"
      ? task.NodeHostname || task.NodeID?.slice(0, 12)
      : task.ServiceName || task.ServiceID.slice(0, 12);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Link
            to={`/tasks/${task.ID}`}
            draggable={false}
          />
        }
        aria-label={`Task ${shortID}, ${task.Status.State}`}
        className={cn(
          "@container absolute min-w-0.5 rounded-sm outline-none hover:brightness-110 focus-visible:ring-2 focus-visible:ring-ring",
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
        <span
          aria-hidden
          className="pointer-events-none absolute inset-0 hidden items-center justify-center px-1 font-mono text-[10px] leading-none text-background @min-[2.5rem]:flex"
        >
          <span className="truncate">{shortID}</span>
        </span>
        {failed && (
          <span className="absolute -inset-y-0.5 -right-px w-0.5 rounded-full bg-status-danger" />
        )}
      </TooltipTrigger>
      <TooltipContent
        side="bottom"
        className="flex flex-col gap-1"
      >
        <span className="flex items-center gap-2">
          <span className="font-mono">{shortID}</span>
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
});

function prunedNote(variant: TimelineVariant, limit: number): string {
  const unit = variant === "global" ? "node" : "replica";

  return `Swarm keeps only the newest ${limit} tasks per ${unit}, so older ones may be missing here.`;
}

interface Selection {
  /** Where the dragged track starts, relative to the timeline. */
  trackLeft: number;
  trackWidth: number;
  from: number;
  to: number;
}

/**
 * Task lifetimes as bars over the page's time range. Dragging across the
 * tracks zooms that range, and a double-click resets it.
 */
export default function TaskTimeline({
  tasks,
  variant,
  timeWindow,
  now,
  retentionLimit,
}: {
  tasks: Task[];
  variant: TimelineVariant;
  timeWindow: TimelineWindow;
  now: number;
  retentionLimit: number;
}) {
  const { customFrom, customTo, setCustomRange, clearCustomRange } = useTimeRange();
  const isCustomRange = customFrom != null && customTo != null;
  const rows = useMemo(
    () => buildTimeline(tasks, variant, timeWindow, now),
    [tasks, variant, timeWindow, now],
  );
  const containerRef = useRef<HTMLDivElement>(null);
  const suppressClickRef = useRef(false);
  const [selection, setSelection] = useState<Selection | null>(null);
  const note = prunedNote(variant, retentionLimit);

  const offsetX = (clientX: number) =>
    clientX - (containerRef.current?.getBoundingClientRect().left ?? 0);

  const startDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return;
    }

    const track = event.currentTarget.getBoundingClientRect();
    const x = offsetX(event.clientX);

    setSelection({ trackLeft: offsetX(track.left), trackWidth: track.width, from: x, to: x });
  };

  // Captured only once it is a drag, so a plain click still reaches its bar.
  const moveDrag = (event: React.PointerEvent<HTMLDivElement>) => {
    if (!selection) {
      return;
    }

    const to = Math.min(
      Math.max(offsetX(event.clientX), selection.trackLeft),
      selection.trackLeft + selection.trackWidth,
    );

    if (Math.abs(to - selection.from) > dragThreshold) {
      event.currentTarget.setPointerCapture(event.pointerId);
    }

    setSelection({ ...selection, to });
  };

  const endDrag = () => {
    setSelection(null);

    if (!selection || Math.abs(selection.to - selection.from) <= dragThreshold) {
      return;
    }

    const span = timeWindow.end - timeWindow.start;
    const timeAt = (x: number) =>
      timeWindow.start + ((x - selection.trackLeft) / selection.trackWidth) * span;

    suppressClickRef.current = true;
    setCustomRange(
      timeAt(Math.min(selection.from, selection.to)) / 1000,
      timeAt(Math.max(selection.from, selection.to)) / 1000,
    );
  };

  return (
    <div
      ref={containerRef}
      className="relative flex flex-col gap-1 rounded-lg border bg-card p-3 select-none"
      onClickCapture={(event) => {
        if (suppressClickRef.current) {
          suppressClickRef.current = false;
          event.preventDefault();
          event.stopPropagation();
        }
      }}
      onDoubleClick={() => {
        if (isCustomRange) {
          clearCustomRange();
        }
      }}
    >
      {rows.length === 0 && (
        <p className="py-2 text-center text-sm text-muted-foreground">
          No tasks in this time range
        </p>
      )}

      {rows.map(({ key, label, bars, lanes, earliest, count }) => {
        const pruned =
          count >= retentionLimit && earliest > timeWindow.start
            ? percentOf(earliest, timeWindow)
            : 0;

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
              className="relative flex-1 cursor-crosshair rounded-sm bg-muted/50"
              style={{ height: `${lanes * 1.25}rem` }}
              onPointerDown={startDrag}
              onPointerMove={moveDrag}
              onPointerUp={endDrag}
              onPointerCancel={() => setSelection(null)}
            >
              {pruned > 0 && (
                <Tooltip>
                  <TooltipTrigger
                    render={<div />}
                    role="img"
                    aria-label={note}
                    className="absolute inset-y-0 left-0 rounded-l-sm bg-[repeating-linear-gradient(135deg,transparent_0_4px,var(--color-border)_4px_5px)]"
                    style={{ width: `${pruned}%` }}
                  />
                  <TooltipContent side="bottom">{note}</TooltipContent>
                </Tooltip>
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

      {selection && (
        <div
          className="pointer-events-none absolute inset-y-0 bg-[var(--chart-zoom)]"
          style={{
            left: Math.min(selection.from, selection.to),
            width: Math.abs(selection.to - selection.from),
          }}
        />
      )}
    </div>
  );
}
