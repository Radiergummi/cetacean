import { loadElk, type Bounds } from "@/lib/layoutElk";
import {
  useEdgesState,
  useNodesInitialized,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import { useCallback, useEffect, useState } from "react";

export interface Graph {
  nodes: Node[];
  edges: Edge[];
}

/** Places a graph from the sizes React Flow has measured. */
export type Layout = (
  nodes: Node[],
  edges: Edge[],
) => Promise<{ nodes: Node[]; edges: Edge[]; bounds: Bounds }>;

/**
 * What the graph is made of, rather than what it says. A caller keys its
 * `ReactFlowProvider` on this so a changed shape is measured afresh, instead of
 * being placed from sizes belonging to nodes that are no longer on screen.
 */
export function graphShape({ nodes, edges }: Graph): string {
  return [
    ...nodes.map(({ id, parentId }) => `${id}:${parentId ?? ""}`),
    ...edges.map(({ id }) => id),
  ].join("|");
}

/**
 * Renders the graph unplaced so React Flow measures it, then lays it out from
 * those measurements. `bounds` stays null until the placement lands, which is
 * when the caller reveals the canvas and fits the viewport.
 */
export function useMeasuredLayout(graph: Graph, layout: Layout) {
  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes);
  const [edges, setEdges] = useEdgesState(graph.edges);
  const [bounds, setBounds] = useState<Bounds | null>(null);
  const measured = useNodesInitialized();
  const { getNodes } = useReactFlow();

  // Started here, the engine downloads while React Flow mounts and measures.
  useEffect(() => {
    loadElk().catch(() => {});
  }, []);

  // A data-only change — a rescaled service, an edited rule — lands on the
  // nodes already placed, leaving the identity of everything that did not move.
  const applyData = useCallback(
    (placed: Node[]) => {
      const data = new Map(graph.nodes.map((node) => [node.id, node.data]));

      return placed.map((node) => {
        const next = data.get(node.id);

        return next && next !== node.data ? { ...node, data: next } : node;
      });
    },
    [graph],
  );

  useEffect(() => {
    setNodes(applyData);
  }, [applyData, setNodes]);

  useEffect(() => {
    if (!measured || bounds) {
      return;
    }

    let live = true;

    // React Flow's store still holds the data as of the last commit, so the
    // layout is handed the current data rather than putting stale data back.
    void layout(applyData(getNodes()), graph.edges).then(
      (result) => {
        if (!live) {
          return;
        }

        setNodes(result.nodes);
        setEdges(result.edges);
        setBounds(result.bounds);
      },
      (error: unknown) => {
        console.warn("graph layout failed:", error);
      },
    );

    return () => {
      live = false;
    };
  }, [measured, bounds, graph, applyData, layout, getNodes, setNodes, setEdges]);

  return { nodes, edges, onNodesChange, bounds };
}
