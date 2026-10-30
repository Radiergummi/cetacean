import type { HistoryEntry } from "../api/types";

/**
 * A row of an activity feed: one change, or a run of gaps no change separates,
 * newest first like the history they come from.
 */
export type ActivityRow =
  | { kind: "change"; entry: HistoryEntry }
  | { kind: "gap"; gaps: HistoryEntry[] };

/**
 * Whether entries hold a change. Gaps are on every resource's timeline, so
 * they alone are no reason to show a list, nor may they use up a page meant
 * for changes; `api.recentActivity` fetches the two apart for that reason.
 */
export function hasChanges(entries: HistoryEntry[]): boolean {
  return entries.some(({ kind }) => kind === "change");
}

/**
 * Groups a newest-first history into rows. Gaps frame changes: a run of them
 * becomes one row, and gaps older than the oldest change are dropped, so a
 * history without changes has no rows. Kinds this client does not know are
 * skipped, as the API asks.
 */
export function activityRows(entries: HistoryEntry[]): ActivityRow[] {
  const rows: ActivityRow[] = [];
  let run: HistoryEntry[] = [];

  for (const entry of entries) {
    if (entry.kind === "gap") {
      run.push(entry);

      continue;
    }

    if (entry.kind !== "change") {
      continue;
    }

    if (run.length > 0) {
      rows.push({ kind: "gap", gaps: run });
      run = [];
    }

    rows.push({ kind: "change", entry });
  }

  return rows;
}
