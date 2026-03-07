import {
  createContext,
  useContext,
  useEffect,
  useState,
  useRef,
  useCallback,
  type ReactNode,
} from "react";

interface SSEEvent {
  type: string;
  action: string;
  id: string;
  resource?: unknown;
}

type SSEListener = (event: SSEEvent) => void;

interface SSEContextValue {
  connected: boolean;
  subscribe: (types: string[], listener: SSEListener) => () => void;
}

const SSEContext = createContext<SSEContextValue | null>(null);

export function SSEProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(true);
  const listenersRef = useRef<Map<symbol, { types: Set<string>; fn: SSEListener }>>(new Map());

  useEffect(() => {
    const es = new EventSource("/api/events");

    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);

    const handler = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as SSEEvent;
        for (const entry of listenersRef.current.values()) {
          if (entry.types.size === 0 || entry.types.has(data.type)) {
            entry.fn(data);
          }
        }
      } catch {
        // ignore malformed events
      }
    };

    // Listen for all named event types used by the app
    const eventTypes = [
      "node",
      "service",
      "task",
      "config",
      "secret",
      "network",
      "volume",
      "stack",
    ];
    for (const type of eventTypes) {
      es.addEventListener(type, handler);
    }

    return () => es.close();
  }, []);

  const subscribe = useCallback((types: string[], listener: SSEListener) => {
    const key = Symbol();
    listenersRef.current.set(key, { types: new Set(types), fn: listener });
    return () => {
      listenersRef.current.delete(key);
    };
  }, []);

  return <SSEContext.Provider value={{ connected, subscribe }}>{children}</SSEContext.Provider>;
}

export function useSSEConnection() {
  const ctx = useContext(SSEContext);
  if (!ctx) throw new Error("useSSEConnection must be used within SSEProvider");
  return ctx.connected;
}

export function useSSESubscribe(types: string[], listener: SSEListener) {
  const ctx = useContext(SSEContext);
  if (!ctx) throw new Error("useSSESubscribe must be used within SSEProvider");
  const listenerRef = useRef(listener);
  listenerRef.current = listener;

  useEffect(() => {
    return ctx.subscribe(types, (e) => listenerRef.current(e));
  }, [ctx, types.join(",")]);
}
