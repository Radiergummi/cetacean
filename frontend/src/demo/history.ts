import type { Dataset } from "./dataset";
import type { HistoryEntry } from "@/api/types";

const minute = 60_000;

/**
 * A few changes per service and node, and one gap, placed relative to `now`
 * so the metric charts of every range have something to mark.
 */
export function demoHistory(dataset: Dataset, now: number): HistoryEntry[] {
  const entries: Omit<HistoryEntry, "id">[] = [
    {
      kind: "gap",
      timestamp: new Date(now - 9 * 60 * minute).toISOString(),
      until: new Date(now - 8 * 60 * minute - 20 * minute).toISOString(),
      type: "",
      action: "",
      resourceId: "",
      name: "",
    },
  ];

  dataset.services.forEach(({ ID, Spec }, index) => {
    const offsets = [35 + index * 7, 4 * 60 + index * 11, 30 * 60 + index * 13];

    offsets.forEach((offset, position) => {
      entries.push({
        kind: "change",
        timestamp: new Date(now - offset * minute).toISOString(),
        type: "service",
        action: "update",
        resourceId: ID,
        name: Spec.Name,
        ...(position === 0
          ? {
              actor: {
                subject: "alice",
                name: "alice",
                provider: "oidc",
                transport: "dashboard" as const,
              },
            }
          : {}),
      });
    });
  });

  dataset.nodes.forEach(({ ID, Description }, index) => {
    entries.push({
      kind: "change",
      timestamp: new Date(now - (2 * 60 + index * 17) * minute).toISOString(),
      type: "node",
      action: "update",
      resourceId: ID,
      name: Description.Hostname ?? ID,
    });
  });

  entries.sort((one, other) => other.timestamp.localeCompare(one.timestamp));

  return entries.map((entry, index) => ({ ...entry, id: entries.length - index }));
}

/**
 * Applies `/history`'s filters: a gap matches every resource filter, and
 * every window it overlaps.
 */
export function filterHistory(entries: HistoryEntry[], params: URLSearchParams): HistoryEntry[] {
  const type = params.get("type");
  const resourceId = params.get("resourceId");
  const kind = params.get("kind");
  const since = params.get("since");
  const until = params.get("until");
  const sinceTime = since ? new Date(since).getTime() : Number.NEGATIVE_INFINITY;
  const untilTime = until ? new Date(until).getTime() : Number.POSITIVE_INFINITY;

  return entries.filter((entry) => {
    if (kind && entry.kind !== kind) {
      return false;
    }

    const start = new Date(entry.timestamp).getTime();

    if (entry.kind === "gap") {
      return new Date(entry.until ?? entry.timestamp).getTime() > sinceTime && start <= untilTime;
    }

    return (
      (!type || entry.type === type) &&
      (!resourceId || entry.resourceId === resourceId) &&
      start > sinceTime &&
      start <= untilTime
    );
  });
}
