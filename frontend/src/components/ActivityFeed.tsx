import type { HistoryEntry } from "../api/types";
import TimeAgo from "./TimeAgo";

interface ActivityFeedProps {
  entries: HistoryEntry[];
  loading?: boolean;
}

function dotColor(action: string): string {
  if (action === "remove") return "bg-red-500";
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
      <div className="absolute left-[11.5px] top-1 bottom-1 w-px bg-border" />
      {entries.map((entry) => (
        <div key={entry.id} className="relative flex items-center gap-3 py-1.5 min-h-8">
          <div
            className={`absolute left-[-13px] top-1/2 -translate-y-1/2 w-2.5 h-2.5 rounded-full ring-2 ring-background ${dotColor(entry.action)}`}
          />
          <div className="flex-1 flex items-center gap-2 min-w-0 text-sm">
            <span className="font-medium truncate">{entry.name}</span>
            <span className="text-muted-foreground">{entry.action}d</span>
            <span className="rounded border px-1.5 py-0.5 text-xs text-muted-foreground">
              {entry.type}
            </span>
          </div>
          <span className="text-xs text-muted-foreground whitespace-nowrap">
            <TimeAgo date={entry.timestamp} />
          </span>
        </div>
      ))}
    </div>
  );
}
