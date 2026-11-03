import type { HistoryActor, HistoryEntry } from "../api/types";

const transportLabels: Record<HistoryActor["transport"], string> = {
  dashboard: "the dashboard",
  rest: "the API",
  mcp: "MCP",
};

/**
 * The phrase naming who made a change, such as "by alice via MCP".
 */
export function attribution({ name, subject, transport }: HistoryActor): string {
  return `by ${name || subject} via ${transportLabels[transport] ?? transport}`;
}

/**
 * A row of an activity feed: a run of changes, an annotation, or a run of
 * gaps nothing else separates, newest first like the history they come from.
 */
export type ActivityRow =
  | { kind: "change"; entries: [HistoryEntry, ...HistoryEntry[]] }
  | { kind: "annotation"; entry: HistoryEntry }
  | { kind: "gap"; gaps: HistoryEntry[] };

/**
 * What decides whether two changes share a row: the same type, name and actor,
 * whatever the action, and both watched or both recovered. A replica restarting
 * in place keeps its name, so its churn folds.
 */
export function runSignature({ actor, name, recovered, type }: HistoryEntry): string {
  return JSON.stringify([
    type,
    name,
    recovered === true,
    actor?.subject,
    actor?.name,
    actor?.provider,
    actor?.client,
    actor?.transport,
  ]);
}

/**
 * Whether entries hold a change or an annotation. Gaps are on every
 * resource's timeline, so they alone are no reason to show a list.
 */
export function hasActivity(entries: HistoryEntry[]): boolean {
  return entries.some(({ kind }) => kind === "change" || kind === "annotation");
}

/**
 * Groups a newest-first history into rows. Gaps frame changes and annotations:
 * a run of them becomes one row, and gaps older than the oldest of those are
 * dropped. Collapsing, a change repeating the one before it joins that row.
 * Unknown kinds are skipped, as the API asks.
 */
export function activityRows(entries: HistoryEntry[], collapse: boolean): ActivityRow[] {
  const rows: ActivityRow[] = [];
  let run: HistoryEntry[] = [];
  const flush = () => {
    if (run.length > 0) {
      rows.push({ kind: "gap", gaps: run });
      run = [];
    }
  };

  for (const entry of entries) {
    if (entry.kind === "gap") {
      run.push(entry);

      continue;
    }

    if (entry.kind === "annotation") {
      flush();
      rows.push({ kind: "annotation", entry });

      continue;
    }

    if (entry.kind !== "change") {
      continue;
    }

    const last = rows.at(-1);

    if (
      collapse &&
      run.length === 0 &&
      last?.kind === "change" &&
      runSignature(last.entries[0]) === runSignature(entry)
    ) {
      last.entries.push(entry);

      continue;
    }

    flush();
    rows.push({ kind: "change", entries: [entry] });
  }

  return rows;
}
