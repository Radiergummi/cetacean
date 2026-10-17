import type { Selection } from "./MeasuredGraph";
import { useUrlParam } from "@/hooks/useUrlParam";

/**
 * The selected node, held in the URL so a graph can be linked to a node of it.
 * Replaces rather than pushes: tabbing across the graph is not a trail of
 * pages to walk back through.
 */
export function useNodeSelection(): Selection {
  return useUrlParam("node");
}
