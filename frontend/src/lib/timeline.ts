import type { HistoryEntry } from "../api/types";
import { attribution } from "./activity";
import { formatDateTime, formatDuration } from "./format";

/**
 * Something on a resource's timeline, in epoch seconds: a point or a range.
 * Changes are points, gaps are ranges, and annotations are either.
 */
export type TimelineItem =
  | { shape: "point"; kind: "change"; at: number; entry: HistoryEntry }
  | { shape: "range"; kind: "gap"; from: number; to: number; entry: HistoryEntry }
  | { shape: "point"; kind: "annotation"; at: number; entry: HistoryEntry }
  | { shape: "range"; kind: "annotation"; from: number; to: number; entry: HistoryEntry };

const pastTense: Record<string, string> = {
  create: "Created",
  update: "Updated",
  remove: "Removed",
};

function seconds(timestamp: string): number {
  return new Date(timestamp).getTime() / 1000;
}

/**
 * The items a resource's charts draw. A service's history holds its tasks'
 * changes too, and only `type`'s own are kept; the server has already chosen
 * the annotations. Kinds this client does not know are skipped, as the API asks.
 */
export function timelineItems(entries: HistoryEntry[], type: string): TimelineItem[] {
  const items: TimelineItem[] = [];

  for (const entry of entries) {
    if (entry.kind === "change" && entry.type === type) {
      const at = seconds(entry.timestamp);

      if (Number.isFinite(at)) {
        items.push({ shape: "point", kind: "change", at, entry });
      }

      continue;
    }

    if (entry.kind === "annotation") {
      const from = seconds(entry.timestamp);

      if (entry.until === undefined) {
        if (Number.isFinite(from)) {
          items.push({ shape: "point", kind: "annotation", at: from, entry });
        }

        continue;
      }

      const to = seconds(entry.until);

      if (Number.isFinite(from) && Number.isFinite(to) && to > from) {
        items.push({ shape: "range", kind: "annotation", from, to, entry });
      }

      continue;
    }

    if (entry.kind === "gap") {
      const from = seconds(entry.timestamp);
      const to = seconds(entry.until ?? entry.timestamp);

      if (Number.isFinite(from) && Number.isFinite(to) && to > from) {
        items.push({ shape: "range", kind: "gap", from, to, entry });
      }
    }
  }

  return items;
}

/**
 * What a change did and, when it was made through Cetacean, who made it:
 * "Updated by alice via MCP".
 */
export function describeChange({ action, actor }: HistoryEntry): string {
  const verb = pastTense[action] ?? action;

  return actor ? `${verb} ${attribution(actor)}` : verb;
}

/**
 * A gap as the activity feed and the charts word it: nobody watched, so
 * nothing was recorded.
 */
export function describeGap(from: number, to: number): { title: string; detail: string } {
  return {
    title: `Not watching for ${formatDuration((to - from) * 1_000_000_000)}`,
    detail: `Nothing recorded from ${formatDateTime(new Date(from * 1000))} to ${formatDateTime(new Date(to * 1000))}`,
  };
}

const dayAndTime = { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" } as const;

/**
 * When an annotation says it happened, hedged because a person wrote it:
 * "about Nov 1, 14:00–14:30". A range dates its end only on another day.
 */
export function describeAnnotationTime(from: number, to?: number | undefined): string {
  const start = new Date(from * 1000);
  const text = `about ${formatDateTime(start, dayAndTime)}`;

  if (to === undefined) {
    return text;
  }

  const end = new Date(to * 1000);

  return start.toDateString() === end.toDateString()
    ? `${text}–${formatDateTime(end, { hour: "2-digit", minute: "2-digit" })}`
    : `${text} – ${formatDateTime(end, dayAndTime)}`;
}
