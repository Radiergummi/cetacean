import dagre from "dagre";
import type { Node, Edge } from "@xyflow/react";

const NODE_WIDTH = 240;
const NODE_HEIGHT = 120;
const GROUP_PADDING = 40;

export function computeLayout(nodes: Node[], edges: Edge[], direction: "LR" | "TB" = "LR"): Node[] {
  const g = new dagre.graphlib.Graph({ compound: true });
  g.setGraph({ rankdir: direction, ranksep: 80, nodesep: 40 });
  g.setDefaultEdgeLabel(() => ({}));

  for (const node of nodes) {
    if (node.type === "group") {
      g.setNode(node.id, { width: NODE_WIDTH + GROUP_PADDING * 2, height: NODE_HEIGHT });
    } else {
      g.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
    }
    if (node.parentId) {
      g.setParent(node.id, node.parentId);
    }
  }

  for (const edge of edges) {
    g.setEdge(edge.source, edge.target);
  }

  dagre.layout(g);

  return nodes.map((node) => {
    const pos = g.node(node.id);
    const width = node.type === "group" ? NODE_WIDTH + GROUP_PADDING * 2 : NODE_WIDTH;
    return {
      ...node,
      position: {
        x: pos.x - width / 2,
        y: pos.y - NODE_HEIGHT / 2,
      },
    };
  });
}
