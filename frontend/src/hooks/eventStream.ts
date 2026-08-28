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
 * Unlike the log tail (see components/log/logStream.ts), these are background
 * streams that nobody is watching, so they retry indefinitely rather than
 * surfacing a "give up" state, and they cannot read the server's Retry-After —
 * that needs fetch, which costs a rewrite of the named-event dispatch these
 * streams rely on.
 */

/** `EventSource.CLOSED`, read as a literal so a stubbed global can't shift it. */
const closedReadyState = 2;

const baseReconnectDelay = 1000;
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
    if (closed) {
      return;
    }

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
