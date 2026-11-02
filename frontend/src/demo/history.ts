import type { Dataset } from "./dataset";
import type { HistoryEntry, NewAnnotation } from "@/api/types";

const minute = 60_000;

const alice = {
  subject: "alice",
  name: "alice",
  provider: "oidc",
  transport: "dashboard",
} as const;

/**
 * Annotations the visitor wrote this session, newest first. Their IDs start
 * high so they never meet the seeded ones, which count up from 1.
 */
const written: HistoryEntry[] = [];
let nextWrittenId = 100000;

/**
 * Records an annotation the visitor wrote, naming each resource by `nameOf`.
 */
export function writeAnnotation(
  body: NewAnnotation,
  nameOf: (type: string, id: string) => string,
): HistoryEntry {
  const entry: HistoryEntry = {
    id: nextWrittenId++,
    kind: "annotation",
    timestamp: body.timestamp,
    ...(body.until ? { until: body.until } : {}),
    type: "",
    action: "",
    resourceId: "",
    name: "",
    text: body.text,
    tags: body.tags,
    ...(body.resources?.length
      ? {
          resources: body.resources.map(({ type, id }) => ({ type, id, name: nameOf(type, id) })),
        }
      : {}),
    actor: visitor,
  };

  written.unshift(entry);

  return entry;
}

/**
 * Edits and deletes the visitor made this session, of any annotation, with
 * each one's version for its ETag; a note never written to is at version 1.
 */
const edits = new Map<number, HistoryEntry>();
const removed = new Set<number>();
const versions = new Map<number, number>();

const visitor = {
  subject: "demo",
  name: "demo",
  provider: "demo",
  transport: "dashboard",
} as const;

export function annotationVersion(id: number): number {
  return versions.get(id) ?? 1;
}

/**
 * Applies a merge patch's text, tags and times to an annotation and keeps it.
 */
export function editAnnotation(
  entry: HistoryEntry,
  patch: { text?: string; tags?: string[] | null; timestamp?: string; until?: string | null },
): HistoryEntry {
  const next: HistoryEntry = {
    ...entry,
    ...(patch.text === undefined ? {} : { text: patch.text }),
    ...("tags" in patch ? { tags: patch.tags ?? [] } : {}),
    ...(patch.timestamp === undefined ? {} : { timestamp: patch.timestamp }),
    updated: new Date().toISOString(),
    editor: visitor,
  };

  if (patch.until) {
    next.until = patch.until;
  } else if ("until" in patch) {
    delete next.until;
  }

  edits.set(entry.id, next);
  versions.set(entry.id, annotationVersion(entry.id) + 1);

  return next;
}

export function removeAnnotation(id: number) {
  removed.add(id);
  versions.set(id, annotationVersion(id) + 1);
}

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

  return [...written, ...entries.map((entry, index) => ({ ...entry, id: entries.length - index }))]
    .filter(({ id }) => !removed.has(id))
    .map((entry) => edits.get(entry.id) ?? entry);
}

/**
 * Applies `/history`'s filters: a gap matches every resource filter and tag,
 * and every window it overlaps. An annotation overlaps windows the same way
 * and matches what it names, or the stack of the service named by
 * `resourceId`; a cluster-wide one matches every resource filter.
 */
export function filterHistory(
  entries: HistoryEntry[],
  params: URLSearchParams,
  stackOf: (id: string) => string | undefined = () => undefined,
): HistoryEntry[] {
  const type = params.get("type");
  const resourceId = params.get("resourceId");
  const tag = params.get("tag");
  const stack = resourceId ? stackOf(resourceId) : undefined;
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
        (!tag || (entry.tags ?? []).includes(tag)) &&
        (named.length === 0 ||
          named.some((resource) =>
            resourceId
              ? resource.id === resourceId || (resource.type === "stack" && resource.id === stack)
              : !type || resource.type === type,
          ))
      );
    }

    if (tag) {
      return false;
    }

    return (
      (!type || entry.type === type) &&
      (!resourceId || entry.resourceId === resourceId) &&
      start > sinceTime &&
      start <= untilTime
    );
  });
}
