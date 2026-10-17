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
    try {
      dispatch(JSON.parse(event.data) as SSEEvent);
    } catch {
      // ignore malformed events
    }
  };

  const batchHandler = (event: MessageEvent) => {
    try {
      const events = JSON.parse(event.data) as SSEEvent[];

      for (const event of events) {
        dispatch(event);
      }
    } catch {
      // ignore parse errors
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
 * Subscribes a listener to the app-wide `/events` stream; returns the unsubscribe.
 */
export type EventsSubscribe = (listener: SSEListener) => () => void;

const SharedEventsContext = createContext<EventsSubscribe | null>(null);

export const SharedEventsProvider = SharedEventsContext.Provider;

/**
 * Opens an EventSource to the given path and dispatches parsed events. `/events`
 * goes through the app-wide stream when one is mounted: every stream holds a
 * slot under the server's cap, and a second one to the same path buys nothing.
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

    const dispatch: SSEListener = (event) => listenerRef.current(event);

    if (path === "/events" && subscribeShared) {
      return subscribeShared(dispatch);
    }

    const stream = openEventStream(apiPath(path), {
      listeners: streamListeners(dispatch),
      onReopened: () => dispatch(reopenedSync),
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
