import type { Selection } from "./MeasuredGraph";
import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

const nodeParam = "node";

/**
 * The selected node, held in the URL so a graph can be linked to a node of it.
 * Replaces rather than pushes: tabbing across the graph is not a trail of
 * pages to walk back through.
 */
export function useNodeSelection(): Selection {
  const [params, setParams] = useSearchParams();

  const select = useCallback(
    (id: string | null) => {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          if (id) {
            next.set(nodeParam, id);
          } else {
            next.delete(nodeParam);
          }

          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  return [params.get(nodeParam), select];
}
