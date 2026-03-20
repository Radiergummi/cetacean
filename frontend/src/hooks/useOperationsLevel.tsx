import type React from "react";
import { createContext, useContext, useEffect, useState } from "react";

interface OperationsLevelState {
  level: number;
  loading: boolean;
}

const OperationsLevelContext = createContext<OperationsLevelState>({
  level: 0,
  loading: true,
});

export function useOperationsLevel() {
  return useContext(OperationsLevelContext);
}

export function OperationsLevelProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<OperationsLevelState>({
    level: 0,
    loading: true,
  });

  useEffect(() => {
    fetch("/-/health")
      .then((response) => response.json())
      .then((data) =>
        setState({
          level: data.operationsLevel ?? 0,
          loading: false,
        }),
      )
      .catch(() =>
        setState({
          level: 0,
          loading: false,
        }),
      );
  }, []);

  return <OperationsLevelContext value={state}>{children}</OperationsLevelContext>;
}
