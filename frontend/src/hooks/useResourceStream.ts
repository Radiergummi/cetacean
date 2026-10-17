import { useLatestRef } from "./useLatestRef";
import { apiPath } from "@/lib/basePath";
import { openEventStream } from "@/lib/eventStream";
import { createContext, useContext, useEffect } from "react";

export interface SSEEvent {
  type: string;
  action: string;
  id: string;
  resource?: unknown | undefined;
}

export type SSEListener = (event: SSEEvent) => void;

export const sseEventTypes = [
  "node",
  "service",
  "task",
  "config",
  "secret",
  "network",
  "volume",
  "stack",
  "sync",
] as const;

/**
 * The SSE listeners that parse each named event, and each batch, into `dispatch`.
 */
export function streamListeners(
  dispatch: SSEListener,
): Record<string, (event: MessageEvent) => void> {
  const handler = (event: MessageEvent) => {
    let parsed: SSEEvent;

    try {
      parsed = JSON.parse(event.data) as SSEEvent;
    } catch {
      return; // ignore malformed events
    }

    dispatch(parsed);
  };

  const batchHandler = (event: MessageEvent) => {
    let events: SSEEvent[];

    try {
      events = JSON.parse(event.data) as SSEEvent[];
    } catch {
      return; // ignore parse errors
    }

    for (const event of events) {
      dispatch(event);
    }
  };

  return {
    ...Object.fromEntries(sseEventTypes.map((type) => [type, handler])),
    batch: batchHandler,
  };
}

/**
 * A stream we reopened ourselves missed everything that happened while it was
 * down, and cannot ask the server to replay it. A sync tells every consumer to
 * refetch, which is what they already do when the server declares one.
 */
export const reopenedSync: SSEEvent = { type: "sync", action: "sync", id: "" };

/**
 * Hands event to each listener, so one that throws neither stops the others
 * nor the rest of a batch.
 */
export function deliver(listeners: Iterable<SSEListener>, event: SSEEvent) {
  for (const listener of listeners) {
    try {
      listener(event);
    } catch (error) {
      console.error("SSE listener failed:", error);
    }
  }
}

/**
 * Subscribes a listener to the shared stream for a path; returns the unsubscribe.
 */
export type EventsSubscribe = (path: string, listener: SSEListener) => () => void;

const SharedEventsContext = createContext<EventsSubscribe | null>(null);

export const SharedEventsProvider = SharedEventsContext.Provider;

/**
 * Opens an EventSource to the given path and dispatches parsed events, through
 * the stream a ConnectionTracker shares for that path when one is mounted:
 * every stream holds a slot under the server's cap, and a second one to the
 * same path buys nothing.
 */
export function useResourceStream(path: string | undefined, listener: SSEListener) {
  const listenerRef = useLatestRef(listener);
  const subscribeShared = useContext(SharedEventsContext);

  useEffect(() => {
    // No path means there is nothing to subscribe to yet — a detail page
    // without a route parameter says so this way.
    if (!path) {
      return;
    }

    const listener: SSEListener = (event) => listenerRef.current(event);

    if (subscribeShared) {
      return subscribeShared(path, listener);
    }

    const stream = openEventStream(apiPath(path), {
      listeners: streamListeners((event) => deliver([listener], event)),
      onReopened: () => deliver([listener], reopenedSync),
    });

    return () => stream.close();
  }, [path, listenerRef, subscribeShared]);
}

const ConnectionContext = createContext<{ connected: boolean; lastEventAt: number | null }>({
  connected: true,
  lastEventAt: null,
});

export const ConnectionProvider = ConnectionContext.Provider;

export function useConnection() {
  return useContext(ConnectionContext);
}
