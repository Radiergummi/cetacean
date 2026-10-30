import type { HistoryActor, HistoryEntry } from "../api/types";
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
 * The kinds this feed draws. The server adds kinds over time, and a client
 * skips the ones it does not know rather than drawing them as changes.
 */
const knownKinds = new Set(["change", "gap"]);

/**
 * A window nobody watched: changes made in it are missing, so it reads as a
 * break in the timeline rather than as a quiet stretch.
 */
function GapDivider({ timestamp, until }: HistoryEntry) {
  const end = until ?? timestamp;
  const length = (new Date(end).getTime() - new Date(timestamp).getTime()) * 1_000_000;

  return (
    <div
      role="separator"
      aria-label={`Nothing recorded from ${formatDateTime(timestamp)} to ${formatDateTime(end)}`}
      title={`Nothing recorded from ${formatDateTime(timestamp)} to ${formatDateTime(end)}`}
      className="relative flex min-h-8 items-center gap-3 py-1.5 ps-3"
    >
      <div className="absolute top-1/2 -left-3.25 h-2.5 w-2.5 -translate-y-1/2 rounded-full border border-dashed border-muted-foreground bg-background ring-2 ring-background" />

      <span className="text-xs leading-none whitespace-nowrap text-muted-foreground">
        <TimeAgo date={end} />
      </span>

      <span className="text-sm whitespace-nowrap text-muted-foreground">
        {length > 0 ? `Not watching for ${formatDuration(length)}` : "Not watching"}
      </span>

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

  const shown = entries.filter(({ kind }) => knownKinds.has(kind));

  if (shown.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">No recent activity</p>;
  }

  return (
    <div className="relative ps-5">
      <div className="absolute top-2.5 bottom-2.5 left-[11.5px] w-px bg-border" />

      {shown.map((entry) => {
        if (entry.kind === "gap") {
          return (
            <GapDivider
              key={entry.id}
              {...entry}
            />
          );
        }

        const { action, actor, id, name, resourceId, timestamp, type } = entry;

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
