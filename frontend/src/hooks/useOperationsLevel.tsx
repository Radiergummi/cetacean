import type React from "react";
import { createContext, useContext, useEffect, useState } from "react";

/**
 * Operations level constants matching the Go config.OperationsLevel enum.
 * Keep in sync with internal/config/config.go.
 */
export const opsLevel = {
  readOnly: 0,
  operational: 1,
  configuration: 2,
  impactful: 3,
} as const;

interface OperationsLevelState {
  level: number;
  loading: boolean;
  error: boolean;
}

const OperationsLevelContext = createContext<OperationsLevelState>({
  level: 0,
  loading: true,
  error: false,
});

export function useOperationsLevel() {
  return useContext(OperationsLevelContext);
}

export function OperationsLevelProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<OperationsLevelState>({
    level: 0,
    loading: true,
    error: false,
  });

  useEffect(() => {
    let cancelled = false;
    let attempt = 0;

    function tryFetch() {
      fetch("/-/health")
        .then((response) => response.json())
        .then((data) => {
          if (!cancelled) {
            setState({
              level: data.operationsLevel ?? 0,
              loading: false,
              error: false,
            });
          }
        })
        .catch(() => {
          if (cancelled) {
            return;
          }

          attempt++;

          if (attempt < 4) {
            const delay = 1000 * 2 ** (attempt - 1);
            setTimeout(tryFetch, delay);
          } else {
            setState({
              level: 0,
              loading: false,
              error: true,
            });
          }
        });
    }

    tryFetch();

    return () => {
      cancelled = true;
    };
  }, []);

  return <OperationsLevelContext value={state}>{children}</OperationsLevelContext>;
}
