/**
 * EventSource with recovery from permanent failures.
 *
 * The browser retries a dropped SSE connection on its own, but only for
 * transport errors: a non-2xx response — such as the 429 every capped stream
 * on this server answers with — puts the connection in CLOSED and it never
 * retries again. Left alone, a resource list or metrics chart that loses this
 * race simply stops updating until the page is reloaded.
 *
 * So this intervenes in exactly one case: the browser has given up. While it
 * is still retrying by itself, its schedule is left untouched.
 *
 * Why these streams keep EventSource while the log tail
 * (components/log/logStream.ts) reads SSE through fetch: EventSource gives
 * them native backoff and Last-Event-ID resume for free, and they want both.
 * The log tail can use neither — it rebuilds its cursor into the request URL
 * on every attempt, and it needs a give-up state and the server's Retry-After
 * to show a person why their tail stopped. These have no per-stream UI, so
 * they retry indefinitely and report nothing but a connected flag.
 */

/** `EventSource.CLOSED`, read as a literal so a stubbed global can't shift it. */
const closedReadyState = 2;

// The capped streams answer 429 with Retry-After: 5, and this helper exists
// mainly for that case. Starting below the interval the server asked for only
// buys two rejected requests, each costing it a full auth and ACL pass at the
// moment it is already at capacity.
const baseReconnectDelay = 5000;
const maxReconnectDelay = 30_000;

export interface EventStreamOptions {
  /** Named SSE event types mapped to their handlers. */
  listeners: Record<string, (event: MessageEvent) => void>;
  onOpen?: (() => void) | undefined;
  /** The connection dropped; a retry follows, by us or by the browser. */
  onDisconnected?: (() => void) | undefined;
}

export interface EventStreamHandle {
  close: () => void;
}

/**
 * Subscribes to an SSE endpoint, reopening it when the browser stops retrying.
 * Returns a handle whose `close` tears down the connection and cancels any
 * pending retry.
 */
export function openEventStream(url: string, options: EventStreamOptions): EventStreamHandle {
  const { listeners, onOpen, onDisconnected } = options;
  let eventSource: EventSource | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let attempt = 0;
  let closed = false;

  const connect = () => {
    const source = new EventSource(url);
    eventSource = source;

    source.onopen = () => {
      attempt = 0;
      onOpen?.();
    };

    source.onerror = () => {
      onDisconnected?.();

      // Still CONNECTING means the browser is handling the retry itself.
      if (source.readyState !== closedReadyState || closed) {
        return;
      }

      source.close();
      attempt += 1;
      retryTimer = setTimeout(
        connect,
        Math.min(baseReconnectDelay * 2 ** (attempt - 1), maxReconnectDelay),
      );
    };

    for (const [type, handler] of Object.entries(listeners)) {
      source.addEventListener(type, handler);
    }
  };

  connect();

  return {
    close: () => {
      closed = true;
      clearTimeout(retryTimer);
      eventSource?.close();
    },
  };
}
