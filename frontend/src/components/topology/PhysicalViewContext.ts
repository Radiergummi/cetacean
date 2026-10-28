import type { NodeMetrics } from "@/hooks/useNodeMetrics";
import type { PlacementUsage } from "@/hooks/usePlacementMetrics";
import { createContext, useContext } from "react";

/**
 * What the physical view overlays on its cards. Kept out of the nodes' data, so
 * a metrics refresh or a network change does not hand React Flow new nodes.
 */
export interface PhysicalViewState {
  network: string | null;
  usage: Record<string, PlacementUsage>;
  nodeMetrics: ((hostname: string, address: string | undefined) => NodeMetrics) | undefined;
}

export const PhysicalViewContext = createContext<PhysicalViewState>({
  network: null,
  usage: {},
  nodeMetrics: undefined,
});

export function usePhysicalView(): PhysicalViewState {
  return useContext(PhysicalViewContext);
}
