import { graphShape } from "@/components/graph/useMeasuredLayout";
import { useNodesState, type Node } from "@xyflow/react";
import { useEffect, useRef } from "react";

/**
 * Holds the physical view's cards so a drag sticks. A refresh with the same
 * cards keeps where the reader put them and takes only the new data; a changed
 * set of cards starts over from the computed grid.
 */
export function usePhysicalNodes(computed: Node[]) {
  const [nodes, setNodes, onNodesChange] = useNodesState(computed);
  const shape = useRef(graphShape({ nodes: computed, edges: [] }));

  useEffect(() => {
    const next = graphShape({ nodes: computed, edges: [] });

    if (next !== shape.current) {
      shape.current = next;
      setNodes(computed);

      return;
    }

    const data = new Map(computed.map((node) => [node.id, node.data]));

    setNodes((placed) =>
      placed.map((node) => {
        const fresh = data.get(node.id);

        return fresh && fresh !== node.data ? { ...node, data: fresh } : node;
      }),
    );
  }, [computed, setNodes]);

  return { nodes, onNodesChange };
}
