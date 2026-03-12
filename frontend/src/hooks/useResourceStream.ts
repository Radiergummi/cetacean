import { createContext, useContext, useEffect, useRef, useState } from "react";

interface SSEEvent {
  type: string;
  action: string;
  id: string;
  resource?: unknown;
}

type SSEListener = (event: SSEEvent) => void;

export const SSE_EVENT_TYPES = [
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
 * Returns connection status for use by ConnectionStatus component.
 */
export function useResourceStream(path: string, listener: SSEListener) {
  const [connected, setConnected] = useState(true);
  const listenerRef = useRef(listener);
  listenerRef.current = listener;

  useEffect(() => {
    const es = new EventSource(path);

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    const handler = (e: MessageEvent) => {
      try {
        listenerRef.current(JSON.parse(e.data) as SSEEvent);
      } catch {
        // ignore malformed events
      }
    };

    const batchHandler = (e: MessageEvent) => {
      try {
        const events = JSON.parse(e.data) as SSEEvent[];
        for (const event of events) {
          listenerRef.current(event);
        }
      } catch {
        // ignore parse errors
      }
    };

    for (const type of SSE_EVENT_TYPES) {
      es.addEventListener(type, handler);
    }
    es.addEventListener("batch", batchHandler);

    return () => es.close();
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
