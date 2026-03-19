import { createContext, type ReactNode, useCallback, useContext, useMemo, useRef } from "react";

type Listener = (timestamp: number) => void;
type IsolationListener = (seriesLabel: string | null) => void;

interface ChartSyncApi {
  subscribe: (chartId: string, listener: Listener) => () => void;
  publish: (chartId: string, timestamp: number) => void;
  subscribeIsolation: (chartId: string, listener: IsolationListener) => () => void;
  publishIsolation: (chartId: string, seriesLabel: string | null) => void;
  clear: () => void;
}

const ChartSyncContext = createContext<ChartSyncApi | null>(null);

export function ChartSyncProvider({ children }: { children: ReactNode }) {
  const listenersRef = useRef<Map<string, Listener>>(new Map());
  const isolationListenersRef = useRef<Map<string, IsolationListener>>(new Map());
  const subscribe = useCallback((chartId: string, listener: Listener) => {
    listenersRef.current.set(chartId, listener);

    return () => {
      listenersRef.current.delete(chartId);
    };
  }, []);

  const publish = useCallback((chartId: string, timestamp: number) => {
    for (const [id, listener] of listenersRef.current) {
      if (id !== chartId) {
        listener(timestamp);
      }
    }
  }, []);

  const subscribeIsolation = useCallback((chartId: string, listener: IsolationListener) => {
    isolationListenersRef.current.set(chartId, listener);

    return () => {
      isolationListenersRef.current.delete(chartId);
    };
  }, []);

  const publishIsolation = useCallback((chartId: string, seriesLabel: string | null) => {
    for (const [id, listener] of isolationListenersRef.current) {
      if (id !== chartId) {
        listener(seriesLabel);
      }
    }
  }, []);

  const clear = useCallback(() => {
    listenersRef.current.clear();
    isolationListenersRef.current.clear();
  }, []);

  const value = useMemo(
    () => ({ subscribe, publish, subscribeIsolation, publishIsolation, clear }),
    [subscribe, publish, subscribeIsolation, publishIsolation, clear],
  );

  return <ChartSyncContext.Provider value={value}>{children}</ChartSyncContext.Provider>;
}

export function useChartSync(): ChartSyncApi {
  const context = useContext(ChartSyncContext);

  if (!context) {
    return {
      subscribe: () => () => {},
      publish: () => {},
      subscribeIsolation: () => () => {},
      publishIsolation: () => {},
      clear: () => {},
    };
  }

  return context;
}
