import type { ApiError, LogLine } from "../../api/client";
import { problemFromResponse } from "../../api/client";
import { backoffDelay } from "../../lib/backoff";

/**
 * Why this reads SSE through fetch instead of using EventSource:
 *
 * The backend caps concurrent SSE connections and answers 429 with a
 * Retry-After header and an RFC 9457 problem body. EventSource exposes neither
 * the status code nor any header to the page — a 429 arrives as a bare `error`
 * event indistinguishable from a dropped connection, and a non-2xx response
 * makes EventSource fail permanently rather than retry.
 *
 * The resource and metrics streams are capped the same way and share the
 * blindness; they keep EventSource and recover via lib/eventStream.ts, which
 * reopens a permanently-failed connection without being able to say why it
 * failed. That is enough for them — see the rationale in that file. This
 * module exists because the log tail is watched by a person who deserves the
 * reason, and because it resumes from a cursor in the URL rather than from
 * Last-Event-ID. Promote it out of `components/log/` if a second consumer
 * ever needs the same treatment.
 */

export const maxReconnectAttempts = 5;
const baseReconnectDelay = 1000;
const maxReconnectDelay = 30_000;

export interface LogStreamFailure {
  /** The server's problem document; null when the transport itself failed. */
  error: ApiError | null;
  retryAfterMilliseconds?: number | undefined;
}

export interface LogStreamRetry {
  /** 1-based index of the attempt about to be made. */
  attempt: number;
  maxAttempts: number;
  /** Epoch milliseconds at which the next attempt fires. */
  retryAt: number;
  failure: LogStreamFailure;
}

export interface ConnectionHandlers {
  onLine: (line: LogLine) => void;
  /** The stream is connected and delivering. */
  onStreaming: () => void;
}

export interface LogStreamHandlers extends ConnectionHandlers {
  /** The stream dropped and another attempt is scheduled. */
  onRetrying: (retry: LogStreamRetry) => void;
  /** The attempt budget is spent; no further attempt will be made. */
  onGaveUp: (failure: LogStreamFailure) => void;
}

export interface LogStreamOutcome {
  /** The `id:` of the last frame seen — the cursor for the next attempt. */
  lastEventId: string | undefined;
  /** Null when the caller aborted, which is not a failure. */
  failure: LogStreamFailure | null;
}

/**
 * Parses a Retry-After value. Only the delta-seconds form is handled, which is
 * what the backend sends.
 */
function parseRetryAfter(value: string | null): number | undefined {
  if (!value) {
    return undefined;
  }

  const seconds = Number(value.trim());

  if (!Number.isFinite(seconds) || seconds < 0) {
    return undefined;
  }

  return seconds * 1000;
}

/**
 * Reads one field out of an SSE frame line. Returns the value when the line
 * carries the wanted field, and undefined otherwise. The backend frames with
 * LF only, so CRLF is not handled.
 */
function fieldValue(line: string, field: string): string | undefined {
  if (!line.startsWith(field) || line[field.length] !== ":") {
    return undefined;
  }

  let start = field.length + 1;

  if (line[start] === " ") {
    start += 1;
  }

  return line.slice(start);
}

/**
 * Handles one SSE frame: emits a line for its `data` field and returns its
 * `id`, which the backend sets to the line's timestamp.
 *
 * Frames are scanned by index rather than split into an array — this runs once
 * per log line, and a busy container produces thousands per second.
 */
function consumeFrame(frame: string, handlers: ConnectionHandlers): string | undefined {
  let data: string | undefined;
  let eventId: string | undefined;
  let position = 0;

  while (position < frame.length) {
    let lineEnd = frame.indexOf("\n", position);

    if (lineEnd === -1) {
      lineEnd = frame.length;
    }

    // Comments (keepalives) start with ':' and carry no field.
    if (frame[position] !== ":") {
      const line = frame.slice(position, lineEnd);

      data = fieldValue(line, "data") ?? data;
      eventId = fieldValue(line, "id") ?? eventId;
    }

    position = lineEnd + 1;
  }

  if (data !== undefined) {
    try {
      handlers.onLine(JSON.parse(data));
    } catch {
      // Skip malformed frames rather than tearing down a working stream.
    }
  }

  return eventId;
}

/**
 * Reads one connection to completion. Resolves when the stream ends, the
 * server refuses it, or the caller aborts.
 */
export async function openLogStream(
  url: string,
  handlers: ConnectionHandlers,
  onActivity: () => void,
  signal: AbortSignal,
): Promise<LogStreamOutcome> {
  let lastEventId: string | undefined;

  try {
    const response = await fetch(url, {
      headers: { Accept: "text/event-stream" },
      credentials: "same-origin",
      signal,
    });

    if (signal.aborted) {
      return { lastEventId, failure: null };
    }

    if (!response.ok) {
      const error = await problemFromResponse(response);

      return {
        lastEventId,
        failure: {
          error,
          retryAfterMilliseconds: parseRetryAfter(response.headers.get("Retry-After")),
        },
      };
    }

    if (!response.body) {
      return { lastEventId, failure: { error: null } };
    }

    handlers.onStreaming();

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    // Bytes before this offset have already been searched for a frame
    // boundary, so a frame spanning many chunks is not rescanned from the
    // start on every read.
    let searchFrom = 0;

    for (;;) {
      // eslint-disable-next-line no-await-in-loop
      const { done, value } = await reader.read();

      if (done) {
        break;
      }

      buffer += decoder.decode(value, { stream: true });

      let frameStart = 0;
      let boundary = buffer.indexOf("\n\n", searchFrom);

      while (boundary !== -1) {
        lastEventId = consumeFrame(buffer.slice(frameStart, boundary), handlers) ?? lastEventId;
        frameStart = boundary + 2;
        boundary = buffer.indexOf("\n\n", frameStart);
      }

      // One slice per chunk rather than one per frame.
      if (frameStart > 0) {
        buffer = buffer.slice(frameStart);
      }

      searchFrom = Math.max(0, buffer.length - 1);
      onActivity();
    }

    return { lastEventId, failure: signal.aborted ? null : { error: null } };
  } catch {
    return { lastEventId, failure: signal.aborted ? null : { error: null } };
  }
}

/** Resolves after the given delay, or as soon as the signal aborts. */
function wait(milliseconds: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, milliseconds);

    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });
}

/**
 * Streams log lines, reconnecting with exponential backoff when the connection
 * drops and honouring the server's Retry-After when it asks for one. Each
 * attempt resumes from the last line delivered, so nothing is replayed or
 * skipped. Returns once the caller aborts or the attempt budget is spent.
 */
export async function streamLogsWithRetry(
  urlFor: (cursor: string) => string,
  initialCursor: string,
  handlers: LogStreamHandlers,
  signal: AbortSignal,
): Promise<void> {
  let cursor = initialCursor;
  let attempt = 0;

  // Any data proves the connection works — including the server's keepalive
  // comments, which is what keeps a quiet service from spending its retry
  // budget. Resetting a local counter costs nothing per chunk; the UI is told
  // separately, once, when a connection opens.
  const onActivity = () => {
    attempt = 0;
  };

  for (;;) {
    if (signal.aborted) {
      return;
    }

    // eslint-disable-next-line no-await-in-loop
    const outcome = await openLogStream(urlFor(cursor), handlers, onActivity, signal);

    cursor = outcome.lastEventId ?? cursor;

    if (!outcome.failure || signal.aborted) {
      return;
    }

    attempt += 1;

    if (attempt > maxReconnectAttempts) {
      handlers.onGaveUp(outcome.failure);

      return;
    }

    const delay =
      outcome.failure.retryAfterMilliseconds ??
      backoffDelay(attempt, { base: baseReconnectDelay, max: maxReconnectDelay });

    handlers.onRetrying({
      attempt,
      maxAttempts: maxReconnectAttempts,
      retryAt: Date.now() + delay,
      failure: outcome.failure,
    });

    // eslint-disable-next-line no-await-in-loop
    await wait(delay, signal);
  }
}
