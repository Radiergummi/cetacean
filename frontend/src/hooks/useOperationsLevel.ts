import { createContext, useContext } from "react";

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

export interface OperationsLevelState {
  level: number;
  loading: boolean;
  error: boolean;
}

export const OperationsLevelContext = createContext<OperationsLevelState>({
  level: 0,
  loading: true,
  error: false,
});

export function useOperationsLevel() {
  return useContext(OperationsLevelContext);
}
