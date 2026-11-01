import type { HistoryEntry } from "../api/types";
import { activityRows, attribution } from "../lib/activity";
import { formatDateTime, formatDuration } from "../lib/format";
import { resourcePath } from "../lib/searchConstants";
import { describeGap } from "../lib/timeline";
import ResourceName from "./ResourceName";
import TimeAgo from "./TimeAgo";
import { ChevronRight } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";

/**
 * How long a gap lasted, in nanoseconds; zero without a valid end.
 */
function gapLength({ timestamp, until }: HistoryEntry): number {
  const length = new Date(until ?? timestamp).getTime() - new Date(timestamp).getTime();

  return Number.isFinite(length) && length > 0 ? length * 1_000_000 : 0;
}

/**
 * One row of the feed: a marker on the timeline, a time, and what happened.
 */
function TimelineRow({
  marker,
  date,
  children,
}: {
  marker: ReactNode;
  date: string;
  children: ReactNode;
}) {
  return (
    <div className="relative flex min-h-8 items-center gap-3 py-1.5 ps-3">
      {marker}

      <span className="text-xs leading-none whitespace-nowrap text-muted-foreground">
        <TimeAgo date={date} />
      </span>

      {children}
    </div>
  );
}

/**
 * A run of windows nobody watched, newest first: changes made in them are
 * missing, so it reads as a break in the timeline rather than a quiet stretch.
 */
function GapDivider({ gaps }: { gaps: HistoryEntry[] }) {
  const newest = gaps[0];
  const oldest = gaps[gaps.length - 1];

  if (newest === undefined || oldest === undefined) {
    return null;
  }

  const end = newest.until ?? newest.timestamp;
  const total = gaps.reduce((sum, gap) => sum + gapLength(gap), 0);

  // Changes a filter hides may sit between merged gaps, so a run names the
  // span it falls in, not one window from its first start to its last end.
  let range = `${gaps.length} stretches between ${formatDateTime(oldest.timestamp)} and ${formatDateTime(end)}`;
  let text = `Not watching ${gaps.length} times${total > 0 ? `, ${formatDuration(total)} in total` : ""}`;

  if (gaps.length === 1) {
    const from = new Date(oldest.timestamp).getTime() / 1000;
    const gap = describeGap(from, from + total / 1_000_000_000);

    range = gap.detail;
    text = total > 0 ? gap.title : "Not watching";
  }

  return (
    <TimelineRow
      date={end}
      marker={
        <div className="absolute top-1/2 -left-3.25 h-2.5 w-2.5 -translate-y-1/2 rounded-full border border-dashed border-muted-foreground bg-background ring-2 ring-background" />
      }
    >
      <span
        className="text-sm whitespace-nowrap text-muted-foreground"
        title={range}
      >
        {text}
        <span className="sr-only">: {range}</span>
      </span>

      <span
        aria-hidden="true"
        className="h-px flex-1 border-t border-dashed border-muted-foreground/50"
      />
    </TimelineRow>
  );
}

/**
 * A run of changes repeating one another, newest first: one row with a count
 * that expands to each change's time. A single change is a plain row.
 */
function ChangeRun({
  entries,
  hideType,
}: {
  entries: [HistoryEntry, ...HistoryEntry[]];
  hideType?: boolean | undefined;
}) {
  const [open, setOpen] = useState(false);
  const [{ action, actor, id, name, resourceId, timestamp, type }] = entries;
  const path = resourcePath(type, resourceId, name);

  return (
    <>
      <TimelineRow
        date={timestamp}
        marker={
          <div
            data-action={action}
            className="absolute top-1/2 -left-3.25 h-2.5 w-2.5 -translate-y-1/2 rounded-full bg-status-ok ring-2 ring-background data-[action=remove]:bg-status-danger"
          />
        }
      >
        <div className="flex min-w-0 flex-1 items-center gap-2 text-sm">
          {!hideType && (
            <span className="rounded border px-1.5 py-0.5 text-xs text-muted-foreground">
              {type}
            </span>
          )}
          {path && action !== "remove" ? (
            <Link
              to={path}
              className="truncate font-medium hover:underline"
            >
              <ResourceName name={name} />
            </Link>
          ) : (
            <span className="truncate font-medium">
              <ResourceName name={name} />
            </span>
          )}
          <span className="text-muted-foreground">{action}d</span>
          {entries.length > 1 && (
            <button
              type="button"
              aria-expanded={open}
              aria-controls={`activity-run-${id}`}
              aria-label={`${entries.length} times, show each`}
              onClick={() => setOpen(!open)}
              className="flex items-center rounded-md border px-1.5 text-xs text-muted-foreground hover:bg-muted"
            >
              ×{entries.length}
              <ChevronRight
                data-open={open || undefined}
                className="h-3 w-3 transition-transform data-open:rotate-90"
              />
            </button>
          )}
          {actor && (
            <span
              className="truncate text-muted-foreground"
              title={`${actor.subject} (${actor.provider}${actor.client ? `, client ${actor.client}` : ""})`}
            >
              {attribution(actor)}
            </span>
          )}
        </div>
      </TimelineRow>

      {open && (
        <ul
          id={`activity-run-${id}`}
          className="ms-6 border-l ps-3"
        >
          {entries.map((entry) => {
            const entryPath = resourcePath(entry.type, entry.resourceId, entry.name);

            return (
              <li
                key={entry.id}
                className="py-0.5 text-xs text-muted-foreground"
              >
                {entryPath && entry.action !== "remove" ? (
                  <Link
                    to={entryPath}
                    className="hover:underline"
                  >
                    <TimeAgo date={entry.timestamp} />
                  </Link>
                ) : (
                  <TimeAgo date={entry.timestamp} />
                )}{" "}
                {entry.action}d
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}

interface ActivityFeedProps {
  entries: HistoryEntry[];
  loading?: boolean | undefined;
  hideType?: boolean | undefined;

  /** Whether changes repeating one another share a row; the full log lists each. */
  collapse?: boolean | undefined;

  /** Shown in place of "No recent activity" when the entries hold no change. */
  emptyText?: string | undefined;
}

export default function ActivityFeed({
  entries,
  loading,
  hideType,
  collapse = true,
  emptyText = "No recent activity",
}: ActivityFeedProps) {
  if (loading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="h-5 w-3/4 rounded bg-muted"
          />
        ))}
      </div>
    );
  }

  const rows = activityRows(entries, collapse);

  if (rows.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">{emptyText}</p>;
  }

  return (
    <div className="relative ps-5">
      <div className="absolute top-2.5 bottom-2.5 left-[11.5px] w-px bg-border" />

      {rows.map((row) => {
        if (row.kind === "gap") {
          return (
            <GapDivider
              key={`gap-${row.gaps[0]?.id}`}
              gaps={row.gaps}
            />
          );
        }

        return (
          <ChangeRun
            key={row.entries[0].id}
            entries={row.entries}
            hideType={hideType}
          />
        );
      })}
    </div>
  );
}
