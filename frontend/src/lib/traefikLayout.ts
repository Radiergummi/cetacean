import { loadElk } from "./layoutElk";
import type { Edge, Node } from "@xyflow/react";
import type { ElkExtendedEdge, ElkNode } from "elkjs/lib/elk-api";

export type RoutedEdgeData = {
  points: { x: number; y: number }[];
};

const layoutOptions = {
  "elk.algorithm": "layered",
  "elk.direction": "RIGHT",
  "elk.edgeRouting": "POLYLINE",
  "elk.layered.spacing.nodeNodeBetweenLayers": "56",
  "elk.spacing.nodeNode": "20",
  "elk.spacing.edgeNode": "24",
  "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
  "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
};

// ELK layers by longest path, which would scatter services across the columns
// their chains happen to end in. Traffic enters at an entrypoint and lands on a
// service, so those two keep their own ends of the graph.
const layerConstraint: Record<string, string | undefined> = {
  traefikEntrypoint: "FIRST",
  traefikService: "LAST",
};

/**
 * Lay the graph out from the sizes React Flow measured, so the boxes ELK packs
 * are the boxes on screen. Its bend points come back on each edge instead of
 * being discarded, which is what keeps a line off the nodes it passes.
 */
export async function layoutTraefikGraph(
  nodes: Node[],
  edges: Edge[],
): Promise<{ nodes: Node[]; edges: Edge[] }> {
  const elk = await loadElk();

  const graph: ElkNode = {
    id: "traefik",
    layoutOptions,
    children: nodes.map((node) => {
      const constraint = layerConstraint[node.type ?? ""];

      return {
        id: node.id,
        width: node.measured?.width ?? 160,
        height: node.measured?.height ?? 40,
        ...(constraint && {
          layoutOptions: { "elk.layered.layering.layerConstraint": constraint },
        }),
      };
    }),
    edges: edges.map((edge): ElkExtendedEdge => ({
      id: edge.id,
      sources: [edge.source],
      targets: [edge.target],
    })),
  };

  const laidOut = await elk.layout(graph);
  const placed = new Map((laidOut.children ?? []).map((child) => [child.id, child]));
  const routed = new Map((laidOut.edges ?? []).map((edge) => [edge.id, edge.sections?.[0]]));

  return {
    nodes: nodes.map((node) => {
      const box = placed.get(node.id);

      return box ? { ...node, position: { x: box.x ?? 0, y: box.y ?? 0 } } : node;
    }),
    edges: edges.map((edge) => {
      const section = routed.get(edge.id);

      if (!section) {
        return edge;
      }

      return {
        ...edge,
        type: "traefikRouted",
        data: {
          points: [section.startPoint, ...(section.bendPoints ?? []), section.endPoint],
        } satisfies RoutedEdgeData,
      };
    }),
  };
}
