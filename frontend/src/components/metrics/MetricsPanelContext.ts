import type { Segment } from "../SegmentedControl";
import { createContext, useContext } from "react";

const ranges = ["1h", "6h", "24h", "7d"] as const;

export const rangeSegments: Segment<string>[] = ranges.map((value) => ({
  label: value.toUpperCase(),
  value,
}));

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
