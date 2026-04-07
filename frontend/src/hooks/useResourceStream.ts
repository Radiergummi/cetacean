import { apiPath } from "@/lib/basePath";
import { createContext, useContext, useEffect, useRef, useState } from "react";

interface SSEEvent {
  type: string;
  action: string;
  id: string;
  resource?: unknown;
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
  const listenerRef = useRef(listener);
  listenerRef.current = listener;

  useEffect(() => {
    const eventSource = new EventSource(apiPath(path));

    eventSource.onopen = () => setConnected(true);
    eventSource.onerror = () => setConnected(false);

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

    for (const type of sseEventTypes) {
      eventSource.addEventListener(type, handler);
    }

    eventSource.addEventListener("batch", batchHandler);

    return () => eventSource.close();
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
