import type { Annotation, AnnotationScope, HistoryEntry, NewAnnotation } from "@/api/types";
import { toLocalInput as isoToLocalInput } from "@/components/log/log-utils";
import { resourcePath } from "@/lib/searchConstants";

const tagPattern = /^[a-z0-9._-]{1,40}$/;

/**
 * A tag as the API takes it, lowercased and trimmed; null when it never would.
 */
export function parseTag(input: string): string | null {
  const tag = input.trim().toLowerCase();

  return tagPattern.test(tag) ? tag : null;
}

/**
 * Epoch seconds as a `datetime-local` value, in local time, to the minute.
 */
export function toLocalInput(seconds: number): string {
  return isoToLocalInput(new Date(seconds * 1000).toISOString());
}

/**
 * A `datetime-local` value, read as local time, as an RFC 3339 instant in UTC.
 */
export function fromLocalInput(value: string): string {
  return new Date(value).toISOString();
}

/**
 * An annotation as a timeline entry, which the note cards render.
 */
export function annotationEntry(note: Annotation): HistoryEntry {
  return { ...note, kind: "annotation", type: "", action: "", resourceId: "", name: "" };
}

/**
 * Whether a refetched note says what the opened one did, apart from which of
 * its resources have since been removed: a 412 for that alone is no conflict.
 */
export function changedOnlyByRemovals(opened: Annotation, current: Annotation): boolean {
  const comparable = (note: Annotation) =>
    JSON.stringify({
      ...note,
      resources: note.resources.map(({ type, id, name }) => ({ type, id, name })),
    });

  return comparable(opened) === comparable(current);
}

/**
 * The `POST /annotations` body for the form's fields.
 */
export function annotationBody({
  text,
  tags,
  from,
  to,
  scope,
}: {
  text: string;
  tags: string[];
  from: string;
  to: string;
  scope: AnnotationScope;
}): NewAnnotation {
  return {
    text,
    tags,
    timestamp: fromLocalInput(from),
    ...(to ? { until: fromLocalInput(to) } : {}),
    ...(scope === "cluster" ? {} : { resources: [{ type: scope.type, id: scope.id }] }),
  };
}

/**
 * What the form edits, as the form holds it.
 */
export interface AnnotationInputs {
  text: string;
  tags: string[];
  from: string;
  to: string;
}

/**
 * A note's fields as the form shows them.
 */
export function annotationInputs({ text, tags, timestamp, until }: Annotation): AnnotationInputs {
  return {
    text,
    tags,
    from: isoToLocalInput(timestamp),
    to: until ? isoToLocalInput(until) : "",
  };
}

/**
 * The merge patch taking `note` to `inputs`: only the fields that differ, as
 * shown, so an untouched time keeps the seconds the form cannot show.
 */
export function annotationPatch(note: Annotation, inputs: AnnotationInputs) {
  const shown = annotationInputs(note);
  const patch: Record<string, unknown> = {};

  if (inputs.text !== shown.text) {
    patch.text = inputs.text;
  }

  if (inputs.tags.join(",") !== shown.tags.join(",")) {
    patch.tags = inputs.tags;
  }

  if (inputs.from !== shown.from) {
    patch.timestamp = fromLocalInput(inputs.from);
  }

  if (inputs.to !== shown.to) {
    patch.until = inputs.to ? fromLocalInput(inputs.to) : null;
  }

  return patch;
}

/**
 * An http or https URL, up to whitespace or a character prose closes it with.
 */
const urlPattern = /\bhttps?:\/\/[^\s<>"]+/gi;

/**
 * A URL without the punctuation a sentence put after it. A closing
 * parenthesis stays while the URL opened one, as in Wikipedia's.
 */
function trimURL(url: string): string {
  let end = url.length;

  while (end > 0) {
    const last = url[end - 1] ?? "";
    const head = url.slice(0, end);

    if (last === ")" && head.split("(").length < head.split(")").length) {
      end--;
    } else if (".,;:!?'\"]".includes(last)) {
      end--;
    } else {
      break;
    }
  }

  return url.slice(0, end);
}

/**
 * A note's text split into plain runs and the URLs inside it.
 */
export function noteSegments(text: string): { text: string; url?: string | undefined }[] {
  const segments: { text: string; url?: string | undefined }[] = [];
  let start = 0;

  for (const match of text.matchAll(urlPattern)) {
    const url = trimURL(match[0]);

    if (match.index > start) {
      segments.push({ text: text.slice(start, match.index) });
    }

    segments.push({ text: url, url });
    start = match.index + url.length;
  }

  if (start < text.length) {
    segments.push({ text: text.slice(start) });
  }

  return segments;
}

/**
 * The least a chart opened on a note shows either side of it, in seconds.
 */
const chartMargin = 30 * 60;

/**
 * The first resource a note names that has a metrics panel, with its window
 * set around the note; undefined for a note on no such resource or yet to come.
 */
export function chartPath(
  { resources = [], timestamp, until }: Pick<HistoryEntry, "resources" | "timestamp" | "until">,
  now = Date.now() / 1000,
): string | undefined {
  const target = resources.find(
    ({ type, removed }) => !removed && (type === "service" || type === "node"),
  );

  const from = new Date(timestamp).getTime() / 1000;

  if (!target || from > now) {
    return undefined;
  }

  const to = until === undefined ? from : new Date(until).getTime() / 1000;
  const margin = Math.max(chartMargin, (to - from) / 4);
  const start = Math.floor(from - margin);
  const end = Math.max(start + 60, Math.ceil(Math.min(to + margin, now)));

  return `${resourcePath(target.type, target.id)}?from=${start}&to=${end}#metrics`;
}
