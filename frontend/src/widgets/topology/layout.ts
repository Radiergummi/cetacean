import type { Edge, Node } from "@xyflow/react";

/** The views get_topology can return, in the order the switcher offers them. */
export const topologyViews = ["network", "placement"] as const;

export type TopologyView = (typeof topologyViews)[number];

/**
 * One vertex, mirroring cluster.TopologyNode. Type is "service", "network" or
 * "node"; group, detail and state are optional because the Go struct omits them
 * when empty.
 */
export interface TopologyNode {
  id: string;
  label: string;
  type: string;
  group?: string;
  detail?: string;
  state?: string;
}

/** One edge, mirroring cluster.TopologyEdge. */
export interface TopologyEdge {
  source: string;
  target: string;
  label?: string;
}

/** The get_topology result, mirroring cluster.TopologyGraph. */
export interface TopologyGraphData {
  view: string;
  nodes: TopologyNode[];
  edges: TopologyEdge[];
}

/**
 * Which node type occupies which column, per view. Both graphs the server
 * builds are bipartite, which is what makes a two-column layout exact rather
 * than an approximation of one.
 */
const networkColumns = ["service", "network"] as const;
const placementColumns = ["node", "service"] as const;

function columnsFor(view: string): readonly string[] {
  return view === "placement" ? placementColumns : networkColumns;
}

const columnGap = 260;
const rowGap = 84;

/**
 * Arranges a topology graph for React Flow.
 *
 * No layout solver: both views are bipartite by construction, so every vertex
 * belongs to a known column and its position is an index, not a search. The
 * dashboard's topology page uses ELK because it lays out nested stack groups;
 * that library is over a megabyte, and a widget is a standalone bundle that
 * would pay for it in full.
 */
export function toFlow(graph: TopologyGraphData): { nodes: Node[]; edges: Edge[] } {
  const columns = columnsFor(graph.view);

  // Count each column's occupants first, so the shorter one can be centred
  // against the taller rather than hanging off the top.
  const heights = graph.nodes.reduce<Record<number, number>>((counts, node) => {
    const column = columnIndex(columns, node.type);

    return { ...counts, [column]: (counts[column] ?? 0) + 1 };
  }, {});

  const tallest = Math.max(...Object.values(heights), 0);
  const rows: Record<number, number> = {};

  const nodes = graph.nodes.map((node): Node => {
    const column = columnIndex(columns, node.type);
    const row = rows[column] ?? 0;
    rows[column] = row + 1;

    const offset = ((tallest - (heights[column] ?? 0)) * rowGap) / 2;

    return {
      id: node.id,
      type: "topologyNode",
      position: { x: column * columnGap, y: offset + row * rowGap },
      data: { ...node },
    };
  });

  const edges = graph.edges.map(
    ({ label, source, target }): Edge => ({
      id: `${source}--${target}`,
      source,
      target,
      label,
      animated: false,
    }),
  );

  return { nodes, edges };
}

/**
 * A type the current view does not place goes in the first column rather than
 * off-canvas: the server and the widget ship separately, so a vertex kind added
 * on one side has to remain visible on the other.
 */
function columnIndex(columns: readonly string[], type: string): number {
  const index = columns.indexOf(type);

  return index < 0 ? 0 : index;
}
