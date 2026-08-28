import type { LogLine } from "../../api/client";
import { encodeLogFrame, keepaliveFrame } from "../../test/mocks";
import type { LogStreamFailure, LogStreamRetry } from "./logStream";
import { maxReconnectAttempts, openLogStream, streamLogsWithRetry } from "./logStream";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/** Builds a Response that streams the given chunks and then ends. */
function streamingResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }

      controller.close();
    },
  });

  return new Response(body, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

function rateLimitedResponse(retryAfterSeconds = 5): Response {
  return new Response(
    JSON.stringify({
      type: "/api/errors/LOG001",
      title: "Too Many Log Streams",
      detail: "too many active log streams",
    }),
    {
      status: 429,
      headers: {
        "Content-Type": "application/problem+json",
        "Retry-After": String(retryAfterSeconds),
      },
    },
  );
}

/** Responses handed out in order, one per connection attempt. */
const queue: (() => Response)[] = [];
const requestedUrls: string[] = [];
let requestInits: (RequestInit | undefined)[] = [];

function queueStream(...chunks: string[]) {
  queue.push(() => streamingResponse(chunks));
}

beforeEach(() => {
  queue.length = 0;
  requestedUrls.length = 0;
  requestInits = [];
  vi.stubGlobal(
    "fetch",
    vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(
      async (input, init) => {
        requestedUrls.push(String(input));
        requestInits.push(init);
        const next = queue.shift();

        return next ? next() : streamingResponse([]);
      },
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function collector() {
  const lines: LogLine[] = [];
  let streaming = 0;

  return {
    lines,
    get streaming() {
      return streaming;
    },
    handlers: {
      onLine: (line: LogLine) => {
        lines.push(line);
      },
      onStreaming: () => {
        streaming += 1;
      },
    },
  };
}

const noop = () => {};

function connect(sink: ReturnType<typeof collector>, onActivity: () => void = noop) {
  return openLogStream("/logs", sink.handlers, onActivity, new AbortController().signal);
}

describe("openLogStream", () => {
  it("emits a log line for each data frame", async () => {
    const sink = collector();
    queueStream(
      encodeLogFrame("2026-01-01T00:00:00Z", "first"),
      encodeLogFrame("2026-01-01T00:00:01Z", "second"),
    );

    await connect(sink);

    expect(sink.lines.map(({ message }) => message)).toEqual(["first", "second"]);
  });

  it("emits every frame when one chunk carries several", async () => {
    const sink = collector();
    queueStream(
      encodeLogFrame("2026-01-01T00:00:00Z", "a") +
        encodeLogFrame("2026-01-01T00:00:01Z", "b") +
        encodeLogFrame("2026-01-01T00:00:02Z", "c"),
    );

    await connect(sink);

    expect(sink.lines.map(({ message }) => message)).toEqual(["a", "b", "c"]);
  });

  it("reassembles a frame split across chunk boundaries", async () => {
    const sink = collector();
    const whole = encodeLogFrame("2026-01-01T00:00:00Z", "split across reads");
    const cut = Math.floor(whole.length / 2);
    queueStream(whole.slice(0, cut), whole.slice(cut));

    await connect(sink);

    expect(sink.lines.map(({ message }) => message)).toEqual(["split across reads"]);
  });

  it("reassembles a long frame arriving in many small chunks", async () => {
    const sink = collector();
    const whole = encodeLogFrame("2026-01-01T00:00:00Z", "x".repeat(500));
    queueStream(...(whole.match(/[\s\S]{1,16}/g) ?? []));

    await connect(sink);

    expect(sink.lines).toHaveLength(1);
    expect(sink.lines[0]?.message).toHaveLength(500);
  });

  it("ignores keepalive comments", async () => {
    const sink = collector();
    queueStream(keepaliveFrame());

    await connect(sink);

    expect(sink.lines).toEqual([]);
  });

  it("counts a keepalive as activity so a quiet stream keeps its retry budget", async () => {
    const sink = collector();
    let activity = 0;
    queueStream(keepaliveFrame());

    await connect(sink, () => {
      activity += 1;
    });

    expect(activity).toBeGreaterThan(0);
  });

  it("reports the id of the most recent frame as the cursor", async () => {
    const sink = collector();
    queueStream(
      encodeLogFrame("2026-01-01T00:00:00Z", "a") + encodeLogFrame("2026-01-01T00:00:09Z", "b"),
    );

    const outcome = await connect(sink);

    expect(outcome.lastEventId).toBe("2026-01-01T00:00:09Z");
  });

  it("skips malformed data frames without aborting the stream", async () => {
    const sink = collector();
    queueStream("data: {not json\n\n" + encodeLogFrame("2026-01-01T00:00:01Z", "survived"));

    await connect(sink);

    expect(sink.lines.map(({ message }) => message)).toEqual(["survived"]);
  });

  it("signals streaming once the response is accepted", async () => {
    const sink = collector();
    queueStream(encodeLogFrame("2026-01-01T00:00:00Z", "x"));

    await connect(sink);

    expect(sink.streaming).toBe(1);
  });

  it("reports a 429 with the server's error code and its Retry-After", async () => {
    const sink = collector();
    queue.push(() => rateLimitedResponse(5));

    const outcome = await connect(sink);

    expect(outcome.failure?.retryAfterMilliseconds).toBe(5000);
    expect(outcome.failure?.error?.status).toBe(429);
    expect(outcome.failure?.error?.code).toBe("LOG001");
    expect(sink.streaming).toBe(0);
  });

  it("reports a non-429 error response with its problem document", async () => {
    const sink = collector();
    queue.push(
      () =>
        new Response(JSON.stringify({ title: "Internal Server Error", detail: "boom" }), {
          status: 500,
        }),
    );

    const outcome = await connect(sink);

    expect(outcome.failure?.error?.status).toBe(500);
    expect(outcome.failure?.error?.title).toBe("Internal Server Error");
    expect(outcome.failure?.retryAfterMilliseconds).toBeUndefined();
  });

  it("reports a transport rejection as a failure with no server error", async () => {
    const sink = collector();
    queue.push(() => {
      throw new TypeError("Failed to fetch");
    });

    const outcome = await connect(sink);

    expect(outcome.failure).toEqual({ error: null });
  });

  it("reports the server closing a healthy stream as a failure so the caller retries", async () => {
    const sink = collector();
    queueStream(encodeLogFrame("2026-01-01T00:00:00Z", "bye"));

    const outcome = await connect(sink);

    expect(sink.lines).toHaveLength(1);
    expect(outcome.failure).toEqual({ error: null });
  });

  it("reports no failure when the caller aborts", async () => {
    const sink = collector();
    const controller = new AbortController();
    queue.push(() => {
      controller.abort();

      throw Object.assign(new Error("aborted"), { name: "AbortError" });
    });

    const outcome = await openLogStream("/logs", sink.handlers, noop, controller.signal);

    expect(outcome.failure).toBeNull();
  });

  it("requests the event-stream content type", async () => {
    const sink = collector();
    queueStream();

    await connect(sink);

    expect(new Headers(requestInits[0]?.headers).get("Accept")).toBe("text/event-stream");
  });
});

describe("streamLogsWithRetry", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  function run() {
    const sink = collector();
    const controller = new AbortController();
    const retries: LogStreamRetry[] = [];
    const gaveUp: LogStreamFailure[] = [];

    const finished = streamLogsWithRetry(
      (cursor) => `/logs?after=${cursor}`,
      "2026-01-01T00:00:00Z",
      {
        ...sink.handlers,
        onRetrying: (retry) => {
          retries.push(retry);
        },
        onGaveUp: (failure) => {
          gaveUp.push(failure);
        },
      },
      controller.signal,
    );

    return { sink, controller, retries, gaveUp, finished };
  }

  it("reconnects after the stream drops", async () => {
    queueStream(encodeLogFrame("2026-01-01T00:00:05Z", "before"));
    const active = run();

    await vi.advanceTimersByTimeAsync(1000);
    active.controller.abort();
    await active.finished;

    expect(requestedUrls.length).toBeGreaterThanOrEqual(2);
  });

  it("resumes from the last line received rather than replaying", async () => {
    queueStream(encodeLogFrame("2026-01-01T00:00:07Z", "last seen"));
    const active = run();

    await vi.advanceTimersByTimeAsync(1000);
    active.controller.abort();
    await active.finished;

    expect(requestedUrls[0]).toBe("/logs?after=2026-01-01T00:00:00Z");
    expect(requestedUrls[1]).toBe("/logs?after=2026-01-01T00:00:07Z");
  });

  it("backs off exponentially across consecutive failures", async () => {
    const active = run();

    await vi.advanceTimersByTimeAsync(120_000);
    await active.finished;

    // Each attempt fires the moment the previous wait elapses, so the gap
    // between consecutive deadlines is the next delay in the curve.
    const waits = active.retries
      .slice(1)
      .map(({ retryAt }, index) => retryAt - active.retries[index]!.retryAt);

    expect(waits).toEqual([2000, 4000, 8000, 16_000]);
  });

  it("waits the server's Retry-After instead of the backoff curve", async () => {
    queue.push(() => rateLimitedResponse(5));
    const active = run();

    await vi.advanceTimersByTimeAsync(0);
    expect(active.retries[0]?.failure.retryAfterMilliseconds).toBe(5000);

    await vi.advanceTimersByTimeAsync(4000);
    expect(requestedUrls).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(1000);
    expect(requestedUrls).toHaveLength(2);

    active.controller.abort();
    await active.finished;
  });

  it("restores the full retry budget once the stream shows activity", async () => {
    queueStream();
    queueStream();
    queueStream(keepaliveFrame());
    const active = run();

    await vi.advanceTimersByTimeAsync(3000);

    expect(active.retries.map(({ attempt }) => attempt)).toEqual([1, 2, 1]);

    active.controller.abort();
    await active.finished;
  });

  it("gives up after the attempt cap", async () => {
    const active = run();

    await vi.advanceTimersByTimeAsync(120_000);
    await active.finished;

    expect(active.retries).toHaveLength(maxReconnectAttempts);
    expect(active.gaveUp).toHaveLength(1);
    expect(requestedUrls).toHaveLength(maxReconnectAttempts + 1);
  });

  it("stops without retrying when the caller aborts", async () => {
    const active = run();

    active.controller.abort();
    await vi.advanceTimersByTimeAsync(10_000);
    await active.finished;

    expect(active.retries).toEqual([]);
    expect(active.gaveUp).toEqual([]);
  });
});
