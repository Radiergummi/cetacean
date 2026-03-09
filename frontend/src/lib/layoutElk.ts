import ELK, { type ElkNode, type ElkExtendedEdge } from "elkjs/lib/elk.bundled.js";
import type { Node, Edge } from "@xyflow/react";

const elk = new ELK();

const NODE_WIDTH = 240;
const NODE_HEIGHT = 120;
const GROUP_PADDING = 20;
const GROUP_HEADER = 36;

const isGroup = (type?: string) => type === "stackGroup" || type === "nodeGroup";

/**
 * Convert React Flow nodes/edges into an ELK graph, run layout, and return
 * positioned React Flow nodes and edges with bend points.
 */
export async function computeLayout(
  nodes: Node[],
  edges: Edge[],
  direction: "RIGHT" | "DOWN" = "RIGHT",
): Promise<{ nodes: Node[]; edges: Edge[] }> {
  // Separate groups from leaf nodes
  const groups = nodes.filter((n) => isGroup(n.type));
  const leaves = nodes.filter((n) => !isGroup(n.type));

  // Build ELK children for each group
  const groupChildren = new Map<string, ElkNode[]>();
  for (const g of groups) {
    groupChildren.set(g.id, []);
  }

  for (const leaf of leaves) {
    const elkNode: ElkNode = {
      id: leaf.id,
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
    };
    if (leaf.parentId && groupChildren.has(leaf.parentId)) {
      groupChildren.get(leaf.parentId)!.push(elkNode);
    }
  }

  // Build top-level ELK children
  const topLevelChildren: ElkNode[] = [];

  for (const g of groups) {
    topLevelChildren.push({
      id: g.id,
      layoutOptions: {
        "elk.padding": `[top=${GROUP_HEADER + GROUP_PADDING},left=${GROUP_PADDING},bottom=${GROUP_PADDING},right=${GROUP_PADDING}]`,
      },
      children: groupChildren.get(g.id) ?? [],
    });
  }

  // Add ungrouped leaf nodes at top level
  for (const leaf of leaves) {
    if (!leaf.parentId) {
      topLevelChildren.push({
        id: leaf.id,
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
      });
    }
  }

  // Build ELK edges (deduplicate: ELK handles one edge per source-target pair)
  // We need to map our per-network edges back after layout.
  const elkEdges: ElkExtendedEdge[] = [];
  const seenPairs = new Set<string>();
  for (const edge of edges) {
    const pairKey = `${edge.source}:${edge.target}`;
    if (seenPairs.has(pairKey)) continue;
    seenPairs.add(pairKey);
    elkEdges.push({
      id: pairKey,
      sources: [edge.source],
      targets: [edge.target],
    });
  }

  const elkGraph: ElkNode = {
    id: "root",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": direction,
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.hierarchyHandling": "INCLUDE_CHILDREN",
      "elk.layered.spacing.nodeNodeBetweenLayers": "80",
      "elk.spacing.nodeNode": "30",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
    },
    children: topLevelChildren,
    edges: elkEdges,
  };

  const layouted = await elk.layout(elkGraph);

  // Extract positions from ELK result
  const positionMap = new Map<string, { x: number; y: number; width: number; height: number }>();

  function extractPositions(elkNode: ElkNode, offsetX = 0, offsetY = 0) {
    const x = (elkNode.x ?? 0) + offsetX;
    const y = (elkNode.y ?? 0) + offsetY;
    positionMap.set(elkNode.id, {
      x,
      y,
      width: elkNode.width ?? 0,
      height: elkNode.height ?? 0,
    });
    if (elkNode.children) {
      for (const child of elkNode.children) {
        extractPositions(child, x, y);
      }
    }
  }

  if (layouted.children) {
    for (const child of layouted.children) {
      extractPositions(child);
    }
  }

  // Extract edge bend points from ELK
  const edgeBendPoints = new Map<string, Array<{ x: number; y: number }>>();
  function extractEdges(elkNode: ElkNode, offsetX = 0, offsetY = 0) {
    // Global position of this node — edge coords are relative to this
    const globalX = (elkNode.x ?? 0) + offsetX;
    const globalY = (elkNode.y ?? 0) + offsetY;

    if (elkNode.edges) {
      for (const edge of elkNode.edges) {
        const points: Array<{ x: number; y: number }> = [];
        for (const section of edge.sections ?? []) {
          points.push({ x: section.startPoint.x + globalX, y: section.startPoint.y + globalY });
          if (section.bendPoints) {
            for (const bp of section.bendPoints) {
              points.push({ x: bp.x + globalX, y: bp.y + globalY });
            }
          }
          points.push({ x: section.endPoint.x + globalX, y: section.endPoint.y + globalY });
        }
        edgeBendPoints.set(edge.id, points);
      }
    }
    if (elkNode.children) {
      for (const child of elkNode.children) {
        extractEdges(child, globalX, globalY);
      }
    }
  }
  extractEdges(layouted);

  // Map back to React Flow nodes
  const resultNodes = nodes.map((node) => {
    const pos = positionMap.get(node.id);
    if (!pos) return node;

    if (isGroup(node.type)) {
      return {
        ...node,
        position: { x: pos.x, y: pos.y },
        style: { width: pos.width, height: pos.height },
      };
    }

    // Child nodes: React Flow expects position relative to parent
    if (node.parentId) {
      const parentPos = positionMap.get(node.parentId);
      if (parentPos) {
        return {
          ...node,
          position: { x: pos.x - parentPos.x, y: pos.y - parentPos.y },
        };
      }
    }

    return {
      ...node,
      position: { x: pos.x, y: pos.y },
    };
  });

  // Map back to React Flow edges, injecting bend points
  const resultEdges = edges.map((edge) => {
    const pairKey = `${edge.source}:${edge.target}`;
    const points = edgeBendPoints.get(pairKey);
    return {
      ...edge,
      zIndex: 10,
      data: {
        ...edge.data,
        bendPoints: points ?? [],
      },
    };
  });

  return { nodes: resultNodes, edges: resultEdges };
}
