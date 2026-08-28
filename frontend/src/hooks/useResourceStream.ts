import { openEventStream } from "./eventStream";
import { useLatestRef } from "./useLatestRef";
import { apiPath } from "@/lib/basePath";
import { createContext, useContext, useEffect, useState } from "react";

interface SSEEvent {
  type: string;
  action: string;
  id: string;
  resource?: unknown | undefined;
}

type SSEListener = (event: SSEEvent) => void;

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
 * Opens an EventSource to the given path and dispatches parsed events.
 * Returns connection status for use by the ConnectionStatus component.
 */
export function useResourceStream(path: string, listener: SSEListener) {
  const [connected, setConnected] = useState(true);
  const listenerRef = useLatestRef(listener);

  useEffect(() => {
    const handler = (event: MessageEvent) => {
      try {
        listenerRef.current(JSON.parse(event.data) as SSEEvent);
      } catch {
        // ignore malformed events
      }
    };

    const batchHandler = (event: MessageEvent) => {
      try {
        const events = JSON.parse(event.data) as SSEEvent[];

        for (const event of events) {
          listenerRef.current(event);
        }
      } catch {
        // ignore parse errors
      }
    };

    const stream = openEventStream(apiPath(path), {
      listeners: {
        ...Object.fromEntries(sseEventTypes.map((type) => [type, handler])),
        batch: batchHandler,
      },
      onOpen: () => setConnected(true),
      onDisconnected: () => setConnected(false),
    });

    return () => stream.close();
  }, [path]);

  return { connected };
}

const ConnectionContext = createContext<{ connected: boolean; lastEventAt: number | null }>({
  connected: true,
  lastEventAt: null,
});

export const ConnectionProvider = ConnectionContext.Provider;

export function useConnection() {
  return useContext(ConnectionContext);
}
