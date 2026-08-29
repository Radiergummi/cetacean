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

import { backoffDelay } from "./backoff";

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
  /**
   * A connection we reopened ourselves is now up, and the events produced
   * while it was down are gone. See the note in `connect` for why the server
   * cannot replay them for us.
   */
  onReopened?: (() => void) | undefined;
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
  const { listeners, onOpen, onDisconnected, onReopened } = options;
  let eventSource: EventSource | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;
  let attempt = 0;
  let closed = false;
  let awaitingVisibility = false;

  // A hidden tab retrying into a capped server holds a connection slot for a
  // view nobody is looking at. Wait for the tab to come back instead, then
  // reconnect at once — the person is looking now.
  const reconnectWhenVisible = () => {
    if (document.visibilityState !== "visible" || closed) {
      return;
    }

    document.removeEventListener("visibilitychange", reconnectWhenVisible);
    awaitingVisibility = false;
    connect(true);
  };

  const connect = (reopening = false) => {
    const source = new EventSource(url);
    eventSource = source;

    source.onopen = () => {
      attempt = 0;
      onOpen?.();

      // The server replays missed events only for a client that presents
      // Last-Event-ID, and that header lives on the EventSource instance the
      // browser retries by itself — a connection we opened in its place sends
      // nothing, so it gets neither the replay nor the full-sync signal sent
      // in its place. Without telling the caller, the page would go green over
      // data frozen at the moment the stream died.
      if (reopening) {
        onReopened?.();
      }
    };

    source.onerror = () => {
      onDisconnected?.();

      // Still CONNECTING means the browser is handling the retry itself.
      if (source.readyState !== closedReadyState || closed) {
        return;
      }

      source.close();
      attempt += 1;

      if (document.visibilityState === "hidden") {
        awaitingVisibility = true;
        document.addEventListener("visibilitychange", reconnectWhenVisible);

        return;
      }

      retryTimer = setTimeout(
        () => connect(true),
        backoffDelay(attempt, { base: baseReconnectDelay, max: maxReconnectDelay }),
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

      if (awaitingVisibility) {
        document.removeEventListener("visibilitychange", reconnectWhenVisible);
        awaitingVisibility = false;
      }

      eventSource?.close();
    },
  };
}
