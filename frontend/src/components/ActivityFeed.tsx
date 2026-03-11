import { Link } from "react-router-dom";
import type { HistoryEntry } from "../api/types";
import { resourcePath } from "../lib/searchConstants";
import TimeAgo from "./TimeAgo";

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
          <div key={i} className="h-5 bg-muted rounded w-3/4" />
        ))}
      </div>
    );
  }

  if (entries.length === 0) {
    return <p className="text-sm text-muted-foreground text-center py-6">No recent activity</p>;
  }

  return (
    <div className="relative pl-5">
      <div className="absolute left-[11.5px] top-2.5 bottom-2.5 w-px bg-border" />

      {entries.map(({ action, id, name, resourceId, timestamp, type }) => (
        <div key={id} className="relative flex items-center gap-3 py-1.5 ps-3 min-h-8">
          <div
            className={`absolute -left-3.25 top-1/2 -translate-y-1/2 w-2.5 h-2.5 rounded-full ring-2 ring-background ${dotColor(
              action,
            )}`}
          />

          <span className="text-xs text-muted-foreground whitespace-nowrap leading-none">
            <TimeAgo date={timestamp} />
          </span>

          <div className="flex-1 flex items-center gap-2 min-w-0 text-sm">
            <span className="rounded border px-1.5 py-0.5 text-xs text-muted-foreground">
              {type}
            </span>
            {(() => {
              const path = resourcePath(type, resourceId, name);
              return path && action !== "remove" ? (
                <Link to={path} className="font-medium truncate hover:underline">
                  {name}
                </Link>
              ) : (
                <span className="font-medium truncate">{name}</span>
              );
            })()}
            <span className="text-muted-foreground">{action}d</span>
          </div>
        </div>
      ))}
    </div>
  );
}
