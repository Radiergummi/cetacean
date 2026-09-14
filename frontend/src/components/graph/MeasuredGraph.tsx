import { RoutedEdge } from "./RoutedEdge";
import { GraphControls, glide, readOnlyKeyboard } from "./viewport";
import { layoutGraph, routedEdgeType, type LayerConstraints } from "@/lib/graphLayout";
import { loadElk } from "@/lib/layoutElk";
import { cn } from "@/lib/utils";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  useEdgesState,
  useNodesInitialized,
  useNodesState,
  useReactFlow,
  type CoordinateExtent,
  type Edge,
  type Node,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

const proOptions = { hideAttribution: true };
const edgeTypes = { [routedEdgeType]: RoutedEdge };

// Generous, because the bound is a hard stop rather than a spring: a fling
// should run out before it lands.
const panMargin = 240;
const maxZoom = 2;

// Until the first fit tells us what "everything visible" costs, allow anything.
const looseZoom = 0.05;

const dimmed = "opacity-15";

interface Graph {
  nodes: Node[];
  edges: Edge[];
}

/** A `useState` pair, so a caller that keeps the selection elsewhere can say so. */
export type Selection = readonly [string | null, (id: string | null) => void];

/** The active node and everything one edge from it. */
function neighbours(edges: Edge[], active: string) {
  const near = new Set([active]);

  for (const edge of edges) {
    if (edge.source === active) {
      near.add(edge.target);
    } else if (edge.target === active) {
      near.add(edge.source);
    }
  }

  return near;
}

/**
 * Nodes are rendered unplaced so React Flow can measure them, and ELK is given
 * those measurements.
 */
function Canvas({
  graph,
  nodeTypes,
  label,
  layerConstraints,
  selection,
}: {
  graph: Graph;
  nodeTypes: NodeTypes;
  label: string;
  layerConstraints?: LayerConstraints | undefined;
  selection?: Selection | undefined;
}) {
  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes);
  const [edges, setEdges] = useEdgesState(graph.edges);
  const [extent, setExtent] = useState<CoordinateExtent | null>(null);
  const [zoomFloor, setZoomFloor] = useState<number | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const own = useState<string | null>(null);
  const measured = useNodesInitialized();
  const { fitView, getNode, getNodes, getZoom, setCenter } = useReactFlow();
  const centred = useRef(false);

  const [selected, select] = selection ?? own;

  // A selection is worth addressing and a hover is not, so only the keyboard's
  // half of the gesture reaches whoever owns the selection.
  const active = hovered ?? selected;

  // Started here, the engine downloads while React Flow mounts and measures.
  useEffect(() => {
    void loadElk();
  }, []);

  // A data-only change — a rescaled service, an edited rule — lands on the
  // nodes already placed. A change of shape remounts instead, via the key.
  const applyData = useCallback(
    (placed: Node[]) => {
      const data = new Map(graph.nodes.map((node) => [node.id, node.data]));

      return placed.map((node) => ({ ...node, data: data.get(node.id) ?? node.data }));
    },
    [graph],
  );

  useEffect(() => {
    setNodes(applyData);
  }, [applyData, setNodes]);

  useEffect(() => {
    if (!measured || extent) {
      return;
    }

    let live = true;

    // React Flow's store still holds the data as of the last commit, so the
    // layout is handed the current data rather than putting stale data back.
    void layoutGraph(applyData(getNodes()), graph.edges, layerConstraints).then((result) => {
      if (!live) {
        return;
      }

      setNodes(result.nodes);
      setEdges(result.edges);
      setExtent([
        [-panMargin, -panMargin],
        [result.bounds.width + panMargin, result.bounds.height + panMargin],
      ]);
    });

    return () => {
      live = false;
    };
  }, [measured, extent, graph, applyData, layerConstraints, getNodes, setNodes, setEdges]);

  // Zoomed out past the fit there is nothing left to see, so what the fit
  // costs is the floor. Taken in the panel, so it never blocks a later one.
  useEffect(() => {
    if (!extent) {
      return;
    }

    void fitView({ ...glide(), duration: 0 }).then(() => setZoomFloor(getZoom() * 0.8));
  }, [extent, fitView, getZoom]);

  // Waits on the fit, which is what decides the zoom the node is seen at.
  useEffect(() => {
    if (zoomFloor == null || centred.current || !selected) {
      return;
    }

    const node = getNode(selected);

    if (!node?.measured?.width || !node.measured.height) {
      return;
    }

    centred.current = true;

    void setCenter(
      node.position.x + node.measured.width / 2,
      node.position.y + node.measured.height / 2,
      glide({ zoom: getZoom() }),
    );
  }, [zoomFloor, selected, getNode, getZoom, setCenter]);

  const near = useMemo(() => (active ? neighbours(edges, active) : null), [active, edges]);

  const shownNodes = useMemo(
    () =>
      nodes.map((node) => ({
        ...node,
        className: cn("transition-opacity", near && !near.has(node.id) && dimmed),
      })),
    [nodes, near],
  );

  const shownEdges = useMemo(
    () =>
      edges.map((edge) => ({
        ...edge,
        className: cn(
          "transition-opacity",
          active && edge.source !== active && edge.target !== active && dimmed,
        ),
      })),
    [edges, active],
  );

  return (
    <div
      data-graph-ready={zoomFloor != null || undefined}
      className="size-full"
    >
      <ReactFlow
        aria-label={label}
        onKeyDown={({ key }) => key === "Escape" && select(null)}
        onFocus={({ target }) => {
          // Focus lands on the link or button inside a node, so the node it
          // belongs to is read off the wrapper React Flow put around it.
          const id = (target as HTMLElement).closest<HTMLElement>(".react-flow__node")?.dataset.id;

          if (id) {
            select(id);
          }
        }}
        nodes={shownNodes}
        edges={shownEdges}
        onNodesChange={onNodesChange}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        proOptions={proOptions}
        {...readOnlyKeyboard}
        nodesDraggable={false}
        elementsSelectable={false}
        // React Flow drops pointer events on a node that is selectable,
        // draggable and listened to by nothing: the hover below is what keeps
        // the links inside a node clickable.
        onNodeMouseEnter={(_, { id }) => setHovered(id)}
        onNodeMouseLeave={() => setHovered(null)}
        onPaneClick={() => select(null)}
        panOnScroll
        minZoom={zoomFloor ?? looseZoom}
        maxZoom={maxZoom}
        {...(extent ? { translateExtent: extent } : {})}
        className="bg-background transition-opacity duration-200"
        style={{ opacity: extent ? 1 : 0 }}
      >
        <Background />
        <GraphControls />
      </ReactFlow>
    </div>
  );
}

/** Seeds the measurement plumbing afresh whenever the graph's shape changes. */
export function MeasuredGraph(props: {
  graph: Graph;
  nodeTypes: NodeTypes;
  label: string;
  layerConstraints?: LayerConstraints | undefined;
  selection?: Selection | undefined;
}) {
  const shape = useMemo(
    () =>
      [...props.graph.nodes.map(({ id }) => id), ...props.graph.edges.map(({ id }) => id)].join(
        "|",
      ),
    [props.graph],
  );

  return (
    <ReactFlowProvider key={shape}>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}
