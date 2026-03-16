import type { HistoryEntry } from "../api/types";
import { resourcePath } from "../lib/searchConstants";
import TimeAgo from "./TimeAgo";
import { Link } from "react-router-dom";

interface ActivityFeedProps {
  entries: HistoryEntry[];
  loading?: boolean;
}

function dotColor(action: string): string {
  if (action === "remove") {
    return "bg-red-500";
  }
  return "bg-green-500";
}

export default function ActivityFeed({ entries, loading }: ActivityFeedProps) {
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

  if (entries.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">No recent activity</p>;
  }

  return (
    <div className="relative pl-5">
      <div className="absolute top-2.5 bottom-2.5 left-[11.5px] w-px bg-border" />

      {entries.map(({ action, id, name, resourceId, timestamp, type }) => (
        <div
          key={id}
          className="relative flex min-h-8 items-center gap-3 py-1.5 ps-3"
        >
          <div
            className={`absolute top-1/2 -left-3.25 h-2.5 w-2.5 -translate-y-1/2 rounded-full ring-2 ring-background ${dotColor(
              action,
            )}`}
          />

          <span className="text-xs leading-none whitespace-nowrap text-muted-foreground">
            <TimeAgo date={timestamp} />
          </span>

          <div className="flex min-w-0 flex-1 items-center gap-2 text-sm">
            <span className="rounded border px-1.5 py-0.5 text-xs text-muted-foreground">
              {type}
            </span>
            {(() => {
              const path = resourcePath(type, resourceId, name);
              return path && action !== "remove" ? (
                <Link
                  to={path}
                  className="truncate font-medium hover:underline"
                >
                  {name}
                </Link>
              ) : (
                <span className="truncate font-medium">{name}</span>
              );
            })()}
            <span className="text-muted-foreground">{action}d</span>
          </div>
        </div>
      ))}
    </div>
  );
}
