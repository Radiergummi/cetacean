import { createContext, useContext, useRef, useCallback, type ReactNode } from "react";

type Listener = (timestamp: number) => void;

interface ChartSyncApi {
  subscribe: (chartId: string, listener: Listener) => () => void;
  publish: (chartId: string, timestamp: number) => void;
  clear: () => void;
}

const ChartSyncContext = createContext<ChartSyncApi | null>(null);

export function ChartSyncProvider({ syncKey: _syncKey, children }: { syncKey: string; children: ReactNode }) {
  const listenersRef = useRef<Map<string, Listener>>(new Map());

  const subscribe = useCallback((chartId: string, listener: Listener) => {
    listenersRef.current.set(chartId, listener);
    return () => { listenersRef.current.delete(chartId); };
  }, []);

  const publish = useCallback((chartId: string, timestamp: number) => {
    for (const [id, listener] of listenersRef.current) {
      if (id !== chartId) listener(timestamp);
    }
  }, []);

  const clear = useCallback(() => { listenersRef.current.clear(); }, []);

  return (
    <ChartSyncContext.Provider value={{ subscribe, publish, clear }}>
      {children}
    </ChartSyncContext.Provider>
  );
}

export function useChartSync(): ChartSyncApi {
  const ctx = useContext(ChartSyncContext);
  if (!ctx) {
    return { subscribe: () => () => {}, publish: () => {}, clear: () => {} };
  }
  return ctx;
}
