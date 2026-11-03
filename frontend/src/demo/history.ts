import type { Dataset } from "./dataset";
import type { HistoryEntry, NewAnnotation } from "@/api/types";
import { splitStackPrefix } from "@/lib/searchConstants";

const minute = 60_000;

const alice = {
  subject: "alice",
  name: "alice",
  provider: "oidc",
  transport: "dashboard",
} as const;

/**
 * The seeded notes' IDs, fixed whatever the dataset holds, as edits are kept
 * by ID; the other seeded entries count up from 1 below them.
 */
export const seededNoteIds = [90001, 90002] as const;

/**
 * Annotations the visitor wrote this session, newest first. Their IDs start
 * above every seeded one.
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
}

/**
 * A few changes per service and node, one gap, and two annotations, placed
 * relative to `now` so the metric charts of every range have something to mark.
 */
export function demoHistory(dataset: Dataset, now: number): HistoryEntry[] {
  const entries: (Omit<HistoryEntry, "id"> & { id?: number })[] = [
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
    id: seededNoteIds[0],
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
      id: seededNoteIds[1],
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

  return [
    ...written,
    ...entries.map((entry, index) => ({ ...entry, id: entry.id ?? entries.length - index })),
  ]
    .filter(({ id }) => !removed.has(id))
    .map((entry) => edits.get(entry.id) ?? entry);
}

/**
 * Applies `/history`'s filters: a gap matches every resource filter and tag,
 * and every window it overlaps. An annotation overlaps windows the same way
 * and matches what it names, the stack of the service named by `resourceId`,
 * or a service of the stack it names; a cluster-wide one matches every filter.
 */
export function filterHistory(
  entries: HistoryEntry[],
  params: URLSearchParams,
  stackOf: (id: string) => string | undefined = () => undefined,
): HistoryEntry[] {
  const type = params.get("type");
  const resourceId = params.get("resourceId");
  const tags = params.getAll("tag").filter(Boolean);
  const matchAny = params.get("matchAny") === "true";
  const authors = params.getAll("author").filter(Boolean);
  const search = params.get("search")?.toLowerCase();
  const mentions = (...fields: (string | undefined)[]) =>
    !search || fields.some((field) => field?.toLowerCase().includes(search));
  const actorFields = ({ actor }: HistoryEntry) => [actor?.name, actor?.subject];
  const stackDisplay = (name: string) => {
    const { prefix, name: rest } = splitStackPrefix(name);

    return prefix === null ? name : `${prefix}/${rest}`;
  };
  const stack = resourceId ? stackOf(resourceId) : undefined;
  const kinds = params.getAll("kind").filter(Boolean);
  const carries = (entry: HistoryEntry) => {
    const carried = (tag: string) => (entry.tags ?? []).includes(tag);

    return tags.length === 0 || (matchAny ? tags.some(carried) : tags.every(carried));
  };
  const byAuthor = ({ actor }: HistoryEntry) =>
    authors.length === 0 || (actor !== undefined && authors.includes(actor.subject));
  const since = params.get("since");
  const until = params.get("until");
  const sinceTime = since ? new Date(since).getTime() : Number.NEGATIVE_INFINITY;
  const untilTime = until ? new Date(until).getTime() : Number.POSITIVE_INFINITY;

  return entries.filter((entry) => {
    if (kinds.length > 0 && !kinds.includes(entry.kind)) {
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
        carries(entry) &&
        byAuthor(entry) &&
        mentions(
          entry.text,
          ...(entry.tags ?? []),
          ...(entry.resources ?? []).flatMap(({ name }) => [name, stackDisplay(name)]),
          ...actorFields(entry),
        ) &&
        (named.length === 0 ||
          named.some(({ type: namedType, id }) =>
            resourceId
              ? id === resourceId ||
                (namedType === "stack" && id === stack) ||
                (type === "stack" && namedType === "service" && stackOf(id) === resourceId)
              : !type || namedType === type,
          ))
      );
    }

    if (tags.length > 0 || !byAuthor(entry)) {
      return false;
    }

    // A stack's history holds its services' changes too.
    const member =
      type === "stack" && entry.type === "service" && stackOf(entry.resourceId) === resourceId;

    return (
      (!type || entry.type === type || member) &&
      (!resourceId || entry.resourceId === resourceId || member) &&
      (mentions(
        entry.name,
        stackDisplay(entry.name),
        entry.type,
        entry.action,
        ...actorFields(entry),
      ) ||
        entry.resourceId.startsWith(search ?? "")) &&
      start > sinceTime &&
      start <= untilTime
    );
  });
}
