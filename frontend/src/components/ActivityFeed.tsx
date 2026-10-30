import type { HistoryActor, HistoryEntry } from "../api/types";
import { activityRows } from "../lib/activity";
import { formatDateTime, formatDuration } from "../lib/format";
import { resourcePath } from "../lib/searchConstants";
import ResourceName from "./ResourceName";
import TimeAgo from "./TimeAgo";
import { Link } from "react-router-dom";

const transportLabels: Record<HistoryActor["transport"], string> = {
  dashboard: "the dashboard",
  rest: "the API",
  mcp: "MCP",
};

/**
 * The phrase naming who made a change, such as "by alice via MCP".
 */
function attribution({ name, subject, transport }: HistoryActor): string {
  return `by ${name || subject} via ${transportLabels[transport] ?? transport}`;
}

/**
 * How long a gap lasted, in nanoseconds; zero without a valid end.
 */
function gapLength({ timestamp, until }: HistoryEntry): number {
  const length = new Date(until ?? timestamp).getTime() - new Date(timestamp).getTime();

  return Number.isFinite(length) && length > 0 ? length * 1_000_000 : 0;
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
  const range = `${formatDateTime(oldest.timestamp)} to ${formatDateTime(end)}`;
  const label =
    gaps.length === 1
      ? `Nothing recorded from ${range}`
      : `Nothing recorded in ${gaps.length} stretches from ${range}`;

  let text = "Not watching";

  if (gaps.length > 1) {
    text = `Not watching ${gaps.length} times${total > 0 ? `, ${formatDuration(total)} in total` : ""}`;
  } else if (total > 0) {
    text = `Not watching for ${formatDuration(total)}`;
  }

  return (
    <div
      role="separator"
      aria-label={label}
      title={label}
      className="relative flex min-h-8 items-center gap-3 py-1.5 ps-3"
    >
      <div className="absolute top-1/2 -left-3.25 h-2.5 w-2.5 -translate-y-1/2 rounded-full border border-dashed border-muted-foreground bg-background ring-2 ring-background" />

      <span className="text-xs leading-none whitespace-nowrap text-muted-foreground">
        <TimeAgo date={end} />
      </span>

      <span className="text-sm whitespace-nowrap text-muted-foreground">{text}</span>

      <span className="h-px flex-1 border-t border-dashed border-muted-foreground/50" />
    </div>
  );
}

interface ActivityFeedProps {
  entries: HistoryEntry[];
  loading?: boolean | undefined;
  hideType?: boolean | undefined;
}

export default function ActivityFeed({ entries, loading, hideType }: ActivityFeedProps) {
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

  const rows = activityRows(entries);

  if (rows.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">No recent activity</p>;
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

        const { action, actor, id, name, resourceId, timestamp, type } = row.entry;

        return (
          <div
            key={id}
            className="relative flex min-h-8 items-center gap-3 py-1.5 ps-3"
          >
            <div
              data-action={action}
              className="absolute top-1/2 -left-3.25 h-2.5 w-2.5 -translate-y-1/2 rounded-full bg-status-ok ring-2 ring-background data-[action=remove]:bg-status-danger"
            />

            <span className="text-xs leading-none whitespace-nowrap text-muted-foreground">
              <TimeAgo date={timestamp} />
            </span>

            <div className="flex min-w-0 flex-1 items-center gap-2 text-sm">
              {!hideType && (
                <span className="rounded border px-1.5 py-0.5 text-xs text-muted-foreground">
                  {type}
                </span>
              )}
              {(() => {
                const path = resourcePath(type, resourceId, name);
                return path && action !== "remove" ? (
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
                );
              })()}
              <span className="text-muted-foreground">{action}d</span>
              {actor && (
                <span
                  className="truncate text-muted-foreground"
                  title={`${actor.subject} (${actor.provider}${actor.client ? `, client ${actor.client}` : ""})`}
                >
                  {attribution(actor)}
                </span>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
