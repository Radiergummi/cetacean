import type { Annotation, AnnotationScope, NewAnnotation } from "@/api/types";
import { toLocalInput as isoToLocalInput } from "@/components/log/log-utils";

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
