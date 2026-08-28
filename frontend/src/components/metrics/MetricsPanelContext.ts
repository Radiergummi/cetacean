import { createContext, useContext } from "react";

export interface MetricsPanelContextValue {
  range: string;
  from?: number | undefined;
  to?: number | undefined;
  refreshKey: number;
  onRangeSelect: (from: number, to: number) => void;
  stacked?: boolean | undefined;
  streaming?: boolean | undefined;
  drillStack?: string | null | undefined;
  setDrillStack?: ((stack: string | null) => void) | undefined;
}

export const MetricsPanelContext = createContext<MetricsPanelContextValue | null>(null);

export function useMetricsPanelContext(): MetricsPanelContextValue | null {
  return useContext(MetricsPanelContext);
}
