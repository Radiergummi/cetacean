import type { AnnotationScope, NewAnnotation } from "@/api/types";
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
