import type { Dataset } from "./dataset";
import type { HistoryEntry } from "@/api/types";

const minute = 60_000;

const alice = {
  subject: "alice",
  name: "alice",
  provider: "oidc",
  transport: "dashboard",
} as const;

/**
 * A few changes per service and node, one gap, and two annotations, placed
 * relative to `now` so the metric charts of every range have something to mark.
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

  const first = dataset.services[0];

  entries.push({
    kind: "annotation",
    timestamp: new Date(now - 3 * 60 * minute).toISOString(),
    until: new Date(now - 3 * 60 * minute + 40 * minute).toISOString(),
    type: "",
    action: "",
    resourceId: "",
    name: "",
    text: "Maintenance window on the storage network",
    tags: ["maintenance"],
    actor: alice,
    updated: new Date(now - 2 * 60 * minute).toISOString(),
    editor: { subject: "bob", name: "bob", provider: "oidc", transport: "rest" },
  });

  if (first) {
    entries.push({
      kind: "annotation",
      timestamp: new Date(now - 50 * minute).toISOString(),
      type: "",
      action: "",
      resourceId: "",
      name: "",
      text: "Raised replicas ahead of the launch",
      tags: ["capacity"],
      resources: [{ type: "service", id: first.ID, name: first.Spec.Name }],
      actor: alice,
    });
  }

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
              actor: alice,
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
 * every window it overlaps. An annotation overlaps windows the same way and
 * matches the resources it names; a cluster-wide one matches every filter.
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

    const overlaps =
      new Date(entry.until ?? entry.timestamp).getTime() > sinceTime && start <= untilTime;

    if (entry.kind === "gap") {
      return overlaps;
    }

    if (entry.kind === "annotation") {
      const named = entry.resources ?? [];

      return (
        overlaps &&
        (named.length === 0 ||
          named.some(
            (resource) =>
              (!type || resource.type === type) && (!resourceId || resource.id === resourceId),
          ))
      );
    }

    return (
      (!type || entry.type === type) &&
      (!resourceId || entry.resourceId === resourceId) &&
      start > sinceTime &&
      start <= untilTime
    );
  });
}
