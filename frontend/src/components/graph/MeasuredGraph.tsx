import { RoutedEdge } from "./RoutedEdge";
import { graphShape, useMeasuredLayout, type Graph } from "./useMeasuredLayout";
import {
  GraphControls,
  glide,
  readOnlyKeyboard,
  useKeptViewport,
  type KeptViewport,
} from "./viewport";
import { layoutGraph, routedEdgeType, type LayerConstraints } from "@/lib/graphLayout";
import { cn } from "@/lib/utils";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  useReactFlow,
  useStore,
  type CoordinateExtent,
  type Edge,
  type Node,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";

const proOptions = { hideAttribution: true };
const edgeTypes = { [routedEdgeType]: RoutedEdge };

// React Flow's own grey is fixed at #b1b1b7 whatever the theme, which reads
// 2.2:1 on a white page — under the 3:1 a graphical object owes. The line and
// the arrowhead it ends in take the theme's colour instead.
const edgeColour = "var(--color-muted-foreground)";
const graphStyle = { "--xy-edge-stroke": edgeColour } as CSSProperties;

// Generous, because the bound is a hard stop rather than a spring: a fling
// should run out before it lands.
const panMargin = 240;
const maxZoom = 2;

// Until the first fit tells us what "everything visible" costs, allow anything.
const looseZoom = 0.05;

const fade = "transition-opacity motion-reduce:transition-none";
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
  viewport,
}: {
  graph: Graph;
  nodeTypes: NodeTypes;
  label: string;
  layerConstraints?: LayerConstraints | undefined;
  selection?: Selection | undefined;
  viewport: KeptViewport;
}) {
  const [zoomFloor, setZoomFloor] = useState<number | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const own = useState<string | null>(null);
  const { fitView, getNode, getViewport, setCenter, setViewport } = useReactFlow();
  const width = useStore((state) => state.width);
  const height = useStore((state) => state.height);
  const restored = useRef(false);
  const shown = useRef<string | null>(null);

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

  // Zoomed out past the fit there is nothing left to see, so what the fit costs
  // is the floor. Re-taken whenever the frame resizes — a window, a sidebar,
  // full screen — since that moves what "everything visible" costs.
  useEffect(() => {
    if (!extent || !width || !height) {
      return;
    }

    void fitView({ ...glide(), duration: 0 }).then(() => {
      setZoomFloor(Math.min(getViewport().zoom, 1) * 0.8);

      // A changed shape remounts the canvas. What the reader had panned and
      // zoomed to outlives that, rather than being thrown away by the refit.
      const kept = restored.current ? null : viewport.take();

      restored.current = true;

      if (kept) {
        void setViewport(kept);
      }
    });
  }, [extent, width, height, fitView, getViewport, setViewport, viewport]);

  // A node selected from the keyboard has to be on screen, or the focus ring
  // lands outside the frame. Only when it is not already there: recentring on
  // every step would swing the graph about under a reader who can see it.
  useEffect(() => {
    if (!selected) {
      shown.current = null;

      return;
    }

    if (zoomFloor == null || shown.current === selected) {
      return;
    }

    const node = getNode(selected);
    const size = node?.measured;

    if (!node || !size?.width || !size.height) {
      return;
    }

    shown.current = selected;

    const { x, y, zoom } = getViewport();
    const left = node.position.x * zoom + x;
    const top = node.position.y * zoom + y;

    if (
      left >= 0 &&
      top >= 0 &&
      left + size.width * zoom <= width &&
      top + size.height * zoom <= height
    ) {
      return;
    }

    void setCenter(
      node.position.x + size.width / 2,
      node.position.y + size.height / 2,
      glide({ zoom }),
    );
  }, [zoomFloor, selected, getNode, getViewport, setCenter, width, height]);

  const near = useMemo(() => (active ? neighbours(edges, active) : null), [active, edges]);

  const shownNodes = useMemo(
    () =>
      nodes.map((node) => ({
        ...node,
        className: cn(fade, near && !near.has(node.id) && dimmed),
      })),
    [nodes, near],
  );

  const shownEdges = useMemo(
    () =>
      edges.map((edge) => ({
        ...edge,
        className: cn(fade, active && edge.source !== active && edge.target !== active && dimmed),
      })),
    [edges, active],
  );

  return (
    <div
      data-graph-ready={zoomFloor != null || undefined}
      className="size-full"
      style={graphStyle}
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
        defaultMarkerColor={edgeColour}
        proOptions={proOptions}
        {...readOnlyKeyboard}
        nodesDraggable={false}
        elementsSelectable={false}
        // React Flow drops pointer events on a node that is selectable,
        // draggable and listened to by nothing: the hover below is what keeps
        // the links inside a node clickable.
        onNodeMouseEnter={(_, { id }) => setHovered(id)}
        onNodeMouseLeave={() => setHovered(null)}
        // Not every browser focuses a button it was clicked on, so the press
        // says what it selected rather than leaving that to the focus above.
        // A link is already on its way elsewhere; rewriting the URL under it
        // would take back the navigation it just made.
        onNodeClick={(event, { id }) => {
          if (!(event.target as HTMLElement).closest("a")) {
            select(id);
          }
        }}
        onPaneClick={() => select(null)}
        onMoveEnd={(event, moved) => {
          // Only what the reader did: a fit reports itself with no event.
          if (event) {
            viewport.keep(moved);
          }
        }}
        panOnScroll
        minZoom={zoomFloor ?? looseZoom}
        maxZoom={maxZoom}
        {...(extent ? { translateExtent: extent } : {})}
        className="bg-background transition-opacity duration-200 motion-reduce:transition-none"
        style={{ opacity: zoomFloor == null ? 0 : 1 }}
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

  // Outside the key, so it survives the remount a changed shape forces.
  const viewport = useKeptViewport();

  return (
    <ReactFlowProvider key={shape}>
      <Canvas
        {...props}
        viewport={viewport}
      />
    </ReactFlowProvider>
  );
}
