import type { AnnotationScope, NewAnnotation } from "@/api/types";

const tagPattern = /^[a-z0-9._-]{1,40}$/;

/**
 * A tag as the API takes it, lowercased and trimmed; null when it never would.
 */
export function parseTag(input: string): string | null {
  const tag = input.trim().toLowerCase();

  return tagPattern.test(tag) ? tag : null;
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

/**
 * Epoch seconds as a `datetime-local` value, in local time, to the minute.
 */
export function toLocalInput(seconds: number): string {
  const date = new Date(seconds * 1000);

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/**
 * A `datetime-local` value, read as local time, as an RFC 3339 instant in UTC.
 */
export function fromLocalInput(value: string): string {
  return new Date(value).toISOString();
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
