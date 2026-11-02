import type { HistoryEntry } from "../api/types";
import { activityRows, attribution, runSignature } from "../lib/activity";
import { formatDateTime, formatDuration } from "../lib/format";
import { resourcePath } from "../lib/searchConstants";
import { describeAnnotationTime, describeGap } from "../lib/timeline";
import ResourceName from "./ResourceName";
import TimeAgo from "./TimeAgo";
import { ChevronRight } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
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
  top,
  children,
}: {
  marker: ReactNode;
  date: string;
  top?: boolean | undefined;
  children: ReactNode;
}) {
  return (
    <div
      data-top={top || undefined}
      className="relative flex min-h-8 items-center gap-3 py-1.5 ps-3 data-top:items-start"
    >
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
 * A run of changes to one resource name, newest first: one row with a count
 * that expands to each change. A single change is a plain row.
 */
function ChangeRun({
  entries,
  hideType,
}: {
  entries: [HistoryEntry, ...HistoryEntry[]];
  hideType?: boolean | undefined;
}) {
  const [open, setOpen] = useState(false);
  const runId = useId();
  const [{ action, actor, name, resourceId, timestamp, type }] = entries;
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
          {entries.length > 1 ? (
            <button
              type="button"
              aria-expanded={open}
              aria-controls={runId}
              onClick={() => setOpen(!open)}
              className="flex items-center gap-0.5 rounded-md border px-1.5 text-xs text-muted-foreground hover:bg-muted"
            >
              {entries.length} changes
              <ChevronRight
                data-open={open || undefined}
                className="h-3 w-3 transition-transform data-open:rotate-90"
              />
            </button>
          ) : (
            <span className="text-muted-foreground">{action}d</span>
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
          id={runId}
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

function seconds(timestamp: string): number {
  return new Date(timestamp).getTime() / 1000;
}

/**
 * A note someone wrote, as a card: its text, when it says it happened, what
 * it is about, its tags, and who wrote it.
 */
function AnnotationCard({ entry }: { entry: HistoryEntry }) {
  const { actor, editor, resources = [], tags = [], text, timestamp, until, updated } = entry;

  return (
    <TimelineRow
      top
      date={timestamp}
      marker={
        <div className="absolute top-3.5 -left-3.25 h-2.5 w-2.5 rounded-sm bg-annotation ring-2 ring-background" />
      }
    >
      <div className="min-w-0 flex-1 rounded-md border border-l-2 border-l-annotation px-2.5 py-1.5 text-sm">
        <p className="break-words whitespace-pre-wrap">{text}</p>

        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span>
            {describeAnnotationTime(
              seconds(timestamp),
              until === undefined ? undefined : seconds(until),
            )}
          </span>

          {resources.length === 0 ? (
            <span>Cluster-wide</span>
          ) : (
            resources.map(({ type, id, name }) => {
              const path = resourcePath(type, id, name);

              return path ? (
                <Link
                  key={`${type}-${id}`}
                  to={path}
                  className="font-medium text-foreground hover:underline"
                >
                  <ResourceName name={name} />
                </Link>
              ) : (
                <span key={`${type}-${id}`}>
                  <ResourceName name={name} />
                </span>
              );
            })
          )}

          {tags.map((tag) => (
            <span
              key={tag}
              className="rounded border px-1"
            >
              #{tag}
            </span>
          ))}

          {actor && <span>{attribution(actor)}</span>}

          {updated && (
            <span
              title={`Edited ${editor ? `${attribution(editor)} ` : ""}${formatDateTime(updated)}`}
            >
              edited
            </span>
          )}
        </div>
      </div>
    </TimelineRow>
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
  const runsSeen = new Map<string, number>();

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

        if (row.kind === "annotation") {
          return (
            <AnnotationCard
              key={`annotation-${row.entry.id}`}
              entry={row.entry}
            />
          );
        }

        // Changes join a run at its newest end and fall off its oldest, so its
        // key counts runs alike from the newest: an open run stays open.
        const signature = runSignature(row.entries[0]);
        const occurrence = runsSeen.get(signature) ?? 0;

        runsSeen.set(signature, occurrence + 1);

        return (
          <ChangeRun
            key={`run-${occurrence}-${signature}`}
            entries={row.entries}
            hideType={hideType}
          />
        );
      })}
    </div>
  );
}
