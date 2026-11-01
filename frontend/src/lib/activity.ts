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
 * A row of an activity feed: a run of changes, or a run of gaps no change
 * separates, newest first like the history they come from.
 */
export type ActivityRow =
  | { kind: "change"; entries: [HistoryEntry, ...HistoryEntry[]] }
  | { kind: "gap"; gaps: HistoryEntry[] };

/**
 * Whether two changes read as the same row: the same type, name, action and
 * actor. Tasks restarting in one slot share a name, so they fold together.
 */
function repeats(previous: HistoryEntry, entry: HistoryEntry): boolean {
  const { actor: previousActor } = previous;
  const { actor } = entry;

  return (
    previous.type === entry.type &&
    previous.name === entry.name &&
    previous.action === entry.action &&
    previousActor?.subject === actor?.subject &&
    previousActor?.name === actor?.name &&
    previousActor?.provider === actor?.provider &&
    previousActor?.client === actor?.client &&
    previousActor?.transport === actor?.transport
  );
}

/**
 * Whether entries hold a change. Gaps are on every resource's timeline, so
 * they alone are no reason to show a list.
 */
export function hasChanges(entries: HistoryEntry[]): boolean {
  return entries.some(({ kind }) => kind === "change");
}

/**
 * Groups a newest-first history into rows. Gaps frame changes: a run of them
 * becomes one row, and gaps older than the oldest change are dropped, so a
 * history without changes has no rows. Collapsing, a change repeating the one
 * before it joins that row. Unknown kinds are skipped, as the API asks.
 */
export function activityRows(entries: HistoryEntry[], collapse: boolean): ActivityRow[] {
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

    const last = rows.at(-1);

    if (
      collapse &&
      run.length === 0 &&
      last?.kind === "change" &&
      repeats(last.entries[0], entry)
    ) {
      last.entries.push(entry);

      continue;
    }

    if (run.length > 0) {
      rows.push({ kind: "gap", gaps: run });
      run = [];
    }

    rows.push({ kind: "change", entries: [entry] });
  }

  return rows;
}
