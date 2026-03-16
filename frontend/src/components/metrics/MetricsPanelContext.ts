import { createContext, useContext } from "react";

export interface MetricsPanelContextValue {
  range: string;
  from?: number;
  to?: number;
  refreshKey: number;
  onRangeSelect: (from: number, to: number) => void;
  stacked?: boolean;
  streaming?: boolean;
  drillStack?: string | null;
  setDrillStack?: (stack: string | null) => void;
}

export const MetricsPanelContext = createContext<MetricsPanelContextValue | null>(null);

export function useMetricsPanelContext(): MetricsPanelContextValue | null {
  return useContext(MetricsPanelContext);
}
