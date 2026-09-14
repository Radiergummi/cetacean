import { RoutedEdge } from "./RoutedEdge";
import { layoutGraph, routedEdgeType, type LayerConstraints } from "@/lib/graphLayout";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  ControlButton,
  Controls,
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
import { Fullscreen, Minimize, Undo2, ZoomIn, ZoomOut } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

const fitViewOptions = { padding: 0.15 };

// Cubic ease-out: quick off the mark, settling rather than stopping. No
// overshoot — the viewport is clamped, so a bounce would be cut short.
const glide = {
  ...fitViewOptions,
  duration: 320,
  ease: (t: number) => 1 - (1 - t) ** 3,
  interpolate: "smooth",
} as const;
const proOptions = { hideAttribution: true };
const edgeTypes = { [routedEdgeType]: RoutedEdge };

// How far past the graph the viewport may be pushed. Generous enough that a
// trackpad fling usually runs out before the bound does, since the bound is a
// hard stop rather than a spring.
const panMargin = 240;
const maxZoom = 2;

// Until the first fit tells us what "everything visible" costs, allow anything.
const looseZoom = 0.05;

const controlButton =
  "border-0 bg-card text-muted-foreground hover:bg-accent hover:text-foreground";

// React Flow's own control CSS loads after Tailwind and fills its icons at
// 12px, which turns a stroked Lucide glyph into a solid blob. Inline wins
// without a specificity fight.
const controlIcon = { fill: "none", width: 16, height: 16, maxWidth: "none", maxHeight: "none" };

export interface Graph {
  nodes: Node[];
  edges: Edge[];
}

/**
 * Nodes are rendered unplaced so React Flow can measure them, and ELK is given
 * those measurements. Laying out from guessed sizes is what left edges hanging
 * in the empty half of an oversized box.
 */
function Canvas({
  graph,
  nodeTypes,
  layerConstraints,
}: {
  graph: Graph;
  nodeTypes: NodeTypes;
  layerConstraints?: LayerConstraints | undefined;
}) {
  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes);
  const [edges, setEdges] = useEdgesState(graph.edges);
  const [extent, setExtent] = useState<CoordinateExtent | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const [zoomFloor, setZoomFloor] = useState(looseZoom);
  const measured = useNodesInitialized();
  const { fitView, getNodes, getZoom, zoomIn, zoomOut } = useReactFlow();
  const shell = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!measured || extent) {
      return;
    }

    let live = true;

    void layoutGraph(getNodes(), graph.edges, layerConstraints).then((result) => {
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
  }, [measured, extent, graph, layerConstraints, getNodes, setNodes, setEdges]);

  // Fitting inside the promise above measured the positions React had not
  // committed yet, which framed whichever node still sat at the origin.
  //
  // What that fit costs is also the floor worth allowing: zoomed out past the
  // point where the whole graph is on screen, there is nothing left to see.
  // Taken in the panel rather than full screen, so it never blocks a later fit.
  useEffect(() => {
    if (!extent) {
      return;
    }

    void fitView(fitViewOptions).then(() => setZoomFloor(getZoom() * 0.8));
  }, [extent, fitView, getZoom]);

  useEffect(() => {
    const onChange = () => {
      setFullscreen(document.fullscreenElement === shell.current);
      void fitView(glide);
    };

    document.addEventListener("fullscreenchange", onChange);

    return () => document.removeEventListener("fullscreenchange", onChange);
  }, [fitView]);

  const toggleFullscreen = useCallback(() => {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void shell.current?.requestFullscreen();
    }
  }, []);

  return (
    <div
      ref={shell}
      className="size-full bg-background"
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        proOptions={proOptions}
        nodesDraggable={false}
        nodesConnectable={false}
        panOnScroll
        minZoom={zoomFloor}
        maxZoom={maxZoom}
        {...(extent ? { translateExtent: extent } : {})}
        className="transition-opacity duration-200"
        style={{ opacity: extent ? 1 : 0 }}
      >
        <Background />
        <Controls
          position="bottom-right"
          orientation="horizontal"
          showZoom={false}
          showFitView={false}
          showInteractive={false}
          className="overflow-hidden rounded-md border bg-card"
          style={{ boxShadow: "none" }}
        >
          <ControlButton
            onClick={() => zoomIn(glide)}
            title="Zoom in"
            aria-label="Zoom in"
            className={controlButton}
          >
            <ZoomIn style={controlIcon} />
          </ControlButton>

          <ControlButton
            onClick={() => zoomOut(glide)}
            title="Zoom out"
            aria-label="Zoom out"
            className={controlButton}
          >
            <ZoomOut style={controlIcon} />
          </ControlButton>

          <ControlButton
            onClick={() => {
              void fitView(glide);
            }}
            title="Reset view"
            aria-label="Reset view"
            className={controlButton}
          >
            <Undo2 style={controlIcon} />
          </ControlButton>

          <ControlButton
            onClick={toggleFullscreen}
            title={fullscreen ? "Exit full screen" : "Full screen"}
            aria-label={fullscreen ? "Exit full screen" : "Full screen"}
            className={controlButton}
          >
            {fullscreen ? <Minimize style={controlIcon} /> : <Fullscreen style={controlIcon} />}
          </ControlButton>
        </Controls>
      </ReactFlow>
    </div>
  );
}

/**
 * The measurement plumbing seeds itself once and has no way to take a second
 * graph, so a caller whose graph can change gives this a `key` that changes
 * with it.
 */
export function MeasuredGraph(props: {
  graph: Graph;
  nodeTypes: NodeTypes;
  layerConstraints?: LayerConstraints | undefined;
}) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}
