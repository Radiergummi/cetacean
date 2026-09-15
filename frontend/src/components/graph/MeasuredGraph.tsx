import { RoutedEdge } from "./RoutedEdge";
import { graphShape, useMeasuredLayout, type Graph } from "./useMeasuredLayout";
import { GraphControls, glide, readOnlyKeyboard } from "./viewport";
import { layoutGraph, routedEdgeType, type LayerConstraints } from "@/lib/graphLayout";
import { cn } from "@/lib/utils";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
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
  const [zoomFloor, setZoomFloor] = useState<number | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const own = useState<string | null>(null);
  const { fitView, getNode, getZoom, setCenter } = useReactFlow();
  const centred = useRef(false);

  const layout = useCallback(
    (placed: Node[], edges: Edge[]) => layoutGraph(placed, edges, layerConstraints),
    [layerConstraints],
  );

  const { nodes, edges, onNodesChange, bounds } = useMeasuredLayout(graph, layout);

  const [selected, select] = selection ?? own;

  // A selection is worth addressing and a hover is not, so only the keyboard's
  // half of the gesture reaches whoever owns the selection.
  const active = hovered ?? selected;

  const extent = useMemo<CoordinateExtent | null>(
    () =>
      bounds && [
        [-panMargin, -panMargin],
        [bounds.width + panMargin, bounds.height + panMargin],
      ],
    [bounds],
  );

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
  const shape = useMemo(() => graphShape(props.graph), [props.graph]);

  return (
    <ReactFlowProvider key={shape}>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}
