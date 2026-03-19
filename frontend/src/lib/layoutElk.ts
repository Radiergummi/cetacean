import type { Edge, Node } from "@xyflow/react";
import ELK, { type ElkExtendedEdge, type ElkNode } from "elkjs/lib/elk.bundled.js";

const elk = new ELK();

const nodeWidth = 224; // matches w-56 (14rem) in ServiceCardNode
const defaultNodeHeight = 120;
const groupPadding = 20;
const groupHeader = 36;

const isGroup = (type?: string) => type === "stackGroup" || type === "nodeGroup";

export async function computeLayout(
  nodes: Node[],
  edges: Edge[],
  direction: "RIGHT" | "DOWN" = "RIGHT",
): Promise<{ nodes: Node[]; edges: Edge[] }> {
  const groups = nodes.filter((n) => isGroup(n.type));
  const leaves = nodes.filter((n) => !isGroup(n.type));

  const groupChildren = new Map<string, ElkNode[]>();

  for (const g of groups) {
    groupChildren.set(g.id, []);
  }

  for (const leaf of leaves) {
    const elkNode: ElkNode = {
      id: leaf.id,
      width: nodeWidth,
      height: ((leaf.data as Record<string, unknown>)?._elkHeight as number) ?? defaultNodeHeight,
    };

    if (leaf.parentId && groupChildren.has(leaf.parentId)) {
      groupChildren.get(leaf.parentId)!.push(elkNode);
    }
  }

  const topLevelChildren: ElkNode[] = [];

  for (const g of groups) {
    topLevelChildren.push({
      id: g.id,
      layoutOptions: {
        "elk.padding": `[top=${groupHeader + groupPadding},left=${groupPadding},bottom=${groupPadding},right=${groupPadding}]`,
        "elk.spacing.edgeNode": "20",
      },
      children: groupChildren.get(g.id) ?? [],
    });
  }

  for (const leaf of leaves) {
    if (!leaf.parentId) {
      topLevelChildren.push({
        id: leaf.id,
        width: nodeWidth,
        height: ((leaf.data as Record<string, unknown>)?._elkHeight as number) ?? defaultNodeHeight,
      });
    }
  }

  // Deduplicate edges for ELK (one per source-target pair)
  const elkEdges: ElkExtendedEdge[] = [];
  const seenPairs = new Set<string>();

  for (const edge of edges) {
    const pairKey = `${edge.source}:${edge.target}`;

    if (seenPairs.has(pairKey)) {
      continue;
    }

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
      "elk.spacing.edgeNode": "20",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
      "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
    },
    children: topLevelChildren,
    edges: elkEdges,
  };

  const layouted = await elk.layout(elkGraph);

  // Build absolute position map for all nodes (ELK positions are relative to parent)
  const positionMap = new Map<string, { x: number; y: number; width: number; height: number }>();

  function extractPositions(elkNode: ElkNode, offsetX: number, offsetY: number) {
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

  for (const child of layouted.children ?? []) {
    extractPositions(child, 0, 0);
  }

  // Extract edge bend points from ELK.
  // With INCLUDE_CHILDREN + hierarchyHandling, ELK places ALL edges on the
  // root node, but edges whose "container" is a group have section coordinates
  // relative to that group. We must add the group's absolute offset.
  const edgeBendPoints = new Map<string, Array<{ x: number; y: number }>>();

  // Build container offset lookup from positionMap
  const containerOffsets = new Map<string, { x: number; y: number }>();

  containerOffsets.set("root", { x: 0, y: 0 });

  for (const g of groups) {
    const position = positionMap.get(g.id);

    if (position) {
      containerOffsets.set(g.id, { x: position.x, y: position.y });
    }
  }

  if (layouted.edges) {
    for (const edge of layouted.edges) {
      const container = (edge as unknown as { container?: string }).container ?? "root";
      const off = containerOffsets.get(container) ?? { x: 0, y: 0 };
      const points: Array<{ x: number; y: number }> = [];

      for (const section of edge.sections ?? []) {
        points.push({
          x: section.startPoint.x + off.x,
          y: section.startPoint.y + off.y,
        });

        if (section.bendPoints) {
          for (const bp of section.bendPoints) {
            points.push({ x: bp.x + off.x, y: bp.y + off.y });
          }
        }

        points.push({
          x: section.endPoint.x + off.x,
          y: section.endPoint.y + off.y,
        });
      }

      edgeBendPoints.set(edge.id, points);
    }
  }

  // Map to React Flow nodes
  const resultNodes = nodes.map((node) => {
    const position = positionMap.get(node.id);

    if (!position) {
      return node;
    }

    if (isGroup(node.type)) {
      return {
        ...node,
        position: {
          x: position.x,
          y: position.y,
        },
        style: {
          width: position.width,
          height: position.height,
        },
      };
    }

    if (node.parentId) {
      const parentPosition = positionMap.get(node.parentId);

      if (parentPosition) {
        return {
          ...node,
          position: {
            x: position.x - parentPosition.x,
            y: position.y - parentPosition.y,
          },
        };
      }
    }

    return {
      ...node,
      position: {
        x: position.x,
        y: position.y,
      },
    };
  });

  // Map to React Flow edges, injecting ELK bend points
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

  return {
    nodes: resultNodes,
    edges: resultEdges,
  };
}
