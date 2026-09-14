import { SelectNodeProvider } from "./NodeChrome";
import { RoutedEdge } from "./RoutedEdge";
import { layoutGraph, routedEdgeType, type LayerConstraints } from "@/lib/graphLayout";
import { loadElk } from "@/lib/layoutElk";
import { cn } from "@/lib/utils";
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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

const fitViewOptions = { padding: 0.15 };

// No overshoot: the viewport is clamped, so a bounce would be cut short.
const glide = {
  ...fitViewOptions,
  duration: 320,
  ease: (t: number) => 1 - (1 - t) ** 3,
  interpolate: "smooth",
} as const;

/** Drops the animation, not the move, so the viewport still lands where it should. */
function eased<T extends { duration: number }>(options: T): T {
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches
    ? { ...options, duration: 0 }
    : options;
}

const proOptions = { hideAttribution: true };
const edgeTypes = { [routedEdgeType]: RoutedEdge };

// Generous, because the bound is a hard stop rather than a spring: a fling
// should run out before it lands.
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

const dimmed = 0.15;

interface Graph {
  nodes: Node[];
  edges: Edge[];
}

/** The active node, everything one edge away, and the edges between them. */
function neighbourhood(edges: Edge[], active: string) {
  const nodes = new Set([active]);
  const touching = new Set<string>();

  for (const edge of edges) {
    if (edge.source === active || edge.target === active) {
      touching.add(edge.id);
      nodes.add(edge.source);
      nodes.add(edge.target);
    }
  }

  return { nodes, touching };
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
  onSelect,
}: {
  graph: Graph;
  nodeTypes: NodeTypes;
  label: string;
  layerConstraints?: LayerConstraints | undefined;
  selection?: string | null | undefined;
  onSelect?: ((id: string | null) => void) | undefined;
}) {
  const [nodes, setNodes, onNodesChange] = useNodesState(graph.nodes);
  const [edges, setEdges] = useEdgesState(graph.edges);
  const [extent, setExtent] = useState<CoordinateExtent | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const [zoomFloor, setZoomFloor] = useState<number | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const [ownSelection, setOwnSelection] = useState<string | null>(null);
  const measured = useNodesInitialized();
  const { fitView, getNode, getNodes, getZoom, setCenter, zoomIn, zoomOut } = useReactFlow();
  const shell = useRef<HTMLDivElement>(null);
  const centred = useRef(false);

  const selected = selection ?? ownSelection;

  // Hover is transient and selection is addressable, so a pointer never writes
  // to the URL and the two cannot disagree while both are set.
  const active = hovered ?? selected;

  const select = useCallback(
    (id: string | null) => {
      setOwnSelection(id);
      onSelect?.(id);
    },
    [onSelect],
  );

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

    void fitView(fitViewOptions).then(() => setZoomFloor(getZoom() * 0.8));
  }, [extent, fitView, getZoom]);

  // A link arriving with a node named puts it in the middle, once the fit has
  // decided the zoom it should be seen at.
  useEffect(() => {
    const node = zoomFloor == null || centred.current ? undefined : getNode(selected ?? "");

    if (!node?.measured?.width || !node.measured.height) {
      return;
    }

    centred.current = true;

    void setCenter(
      node.position.x + node.measured.width / 2,
      node.position.y + node.measured.height / 2,
      eased({ ...glide, zoom: getZoom() }),
    );
  }, [zoomFloor, selected, getNode, getZoom, setCenter]);

  useEffect(() => {
    const onChange = () => {
      setFullscreen(document.fullscreenElement === shell.current);
      void fitView(eased(glide));
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

  const near = useMemo(() => (active ? neighbourhood(edges, active) : null), [active, edges]);

  const shownNodes = useMemo(
    () =>
      nodes.map((node) => ({
        ...node,
        className: cn("transition-opacity", near && !near.nodes.has(node.id) && "opacity-15"),
      })),
    [nodes, near],
  );

  const shownEdges = useMemo(
    () =>
      edges.map((edge) => ({
        ...edge,
        style: {
          ...edge.style,
          transition: "opacity 200ms",
          ...(near && !near.touching.has(edge.id) && { opacity: dimmed }),
        },
      })),
    [edges, near],
  );

  return (
    <div
      ref={shell}
      data-graph-ready={zoomFloor != null || undefined}
      className="size-full bg-background"
    >
      <SelectNodeProvider value={select}>
        <ReactFlow
          aria-label={label}
          onKeyDown={({ key }) => key === "Escape" && select(null)}
          nodes={shownNodes}
          edges={shownEdges}
          onNodesChange={onNodesChange}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          proOptions={proOptions}
          nodesDraggable={false}
          nodesConnectable={false}
          nodesFocusable={false}
          edgesFocusable={false}
          elementsSelectable={false}
          disableKeyboardA11y
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
              onClick={() => zoomIn(eased(glide))}
              title="Zoom in"
              aria-label="Zoom in"
              className={controlButton}
            >
              <ZoomIn style={controlIcon} />
            </ControlButton>

            <ControlButton
              onClick={() => zoomOut(eased(glide))}
              title="Zoom out"
              aria-label="Zoom out"
              className={controlButton}
            >
              <ZoomOut style={controlIcon} />
            </ControlButton>

            <ControlButton
              onClick={() => {
                void fitView(eased(glide));
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
      </SelectNodeProvider>
    </div>
  );
}

/** Seeds the measurement plumbing afresh whenever the graph's shape changes. */
export function MeasuredGraph(props: {
  graph: Graph;
  nodeTypes: NodeTypes;
  label: string;
  layerConstraints?: LayerConstraints | undefined;
  selection?: string | null | undefined;
  onSelect?: ((id: string | null) => void) | undefined;
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
