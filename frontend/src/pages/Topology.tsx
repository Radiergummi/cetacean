import { api } from "../api/client";
import type { JGFGraph } from "../api/types";
import EmptyState from "../components/EmptyState";
import {
  FitOnResize,
  glide,
  GraphControls,
  readOnlyKeyboard,
  useKeptViewport,
  type KeptViewport,
} from "../components/graph/viewport";
import "@xyflow/react/dist/style.css";
import { LoadingPage } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import SegmentedControl from "../components/SegmentedControl";
import GroupNode from "../components/topology/GroupNode";
import { HighlightProvider } from "../components/topology/HighlightContext";
import NetworkEdge from "../components/topology/NetworkEdge";
import PhysicalNodeCard from "../components/topology/PhysicalNodeCard";
import { PhysicalViewContext } from "../components/topology/PhysicalViewContext";
import ServiceCardNode from "../components/topology/ServiceCardNode";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { useMatchesBreakpoint } from "../hooks/useMatchesBreakpoint";
import { isNodeExporterReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useNodeMetrics } from "../hooks/useNodeMetrics";
import { usePlacementMetrics } from "../hooks/usePlacementMetrics";
import { useUrlParam } from "../hooks/useUrlParam";
import { computeLayout } from "../lib/layoutElk";
import {
  networkGraphToReactFlow,
  placementGraphToReactFlow,
  placementNetworks,
  stackColors,
} from "../lib/topologyTransform";
import { getErrorMessage } from "../lib/utils";
import { graphShape, useMeasuredLayout, type Graph } from "@/components/graph/useMeasuredLayout";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ReactFlow, ReactFlowProvider, Background, useReactFlow } from "@xyflow/react";
import { Info, Network, Server, X } from "lucide-react";
import { useState, useEffect, useMemo } from "react";

const logicalNodeTypes = {
  stackGroup: GroupNode,
  serviceCard: ServiceCardNode,
};
const logicalEdgeTypes = { networkEdge: NetworkEdge };
const physicalNodeTypes = { physicalNode: PhysicalNodeCard };

type View = "logical" | "physical";

function StackLegend({ colors, isMobile }: { colors: Map<string, string>; isMobile: boolean }) {
  const [open, setOpen] = useState(!isMobile);

  if (colors.size === 0) {
    return null;
  }

  if (isMobile && !open) {
    return (
      <button
        aria-label="Show legend"
        type="button"
        onClick={() => setOpen(true)}
        className="absolute right-3 bottom-3 z-10 rounded-lg border bg-card/90 p-2 shadow-sm backdrop-blur-sm"
        title="Show legend"
      >
        <Info className="size-4 text-muted-foreground" />
      </button>
    );
  }

  return (
    <div className="absolute bottom-3 left-3 z-10 rounded-lg border bg-card/90 p-3 text-xs shadow-sm backdrop-blur-sm">
      <div className="mb-1.5 flex items-center justify-between">
        <span className="font-medium text-muted-foreground">Stacks</span>
        {isMobile && (
          <button
            aria-label="Hide legend"
            type="button"
            onClick={() => setOpen(false)}
            className="ms-2 text-muted-foreground hover:text-foreground"
          >
            <X className="size-3" />
          </button>
        )}
      </div>
      <div className="flex flex-col gap-1">
        {[...colors.entries()].map(([stack, color]) => (
          <span
            key={stack}
            className="flex items-center gap-1.5"
          >
            <span
              className="inline-block size-3 shrink-0 rounded-full"
              style={{ backgroundColor: color }}
            />
            {stack}
          </span>
        ))}
      </div>
    </div>
  );
}

function LogicalCanvas({ graph, viewport }: { graph: Graph; viewport: KeptViewport }) {
  const { nodes, edges, onNodesChange, bounds } = useMeasuredLayout(graph, computeLayout);
  const { fitView, setViewport } = useReactFlow();

  // Live updates change the shape often, and a changed shape remounts the
  // canvas. What the reader had panned and zoomed to outlives that, so only a
  // graph they have not moved yet is fitted.
  useEffect(() => {
    if (!bounds) {
      return;
    }

    const kept = viewport.take();

    if (kept) {
      void setViewport(kept);
    } else {
      void fitView(glide());
    }
  }, [bounds, fitView, setViewport, viewport]);

  return (
    <ReactFlow
      aria-label="Cluster network topology"
      className="bg-background transition-opacity duration-200"
      style={{ opacity: bounds ? 1 : 0 }}
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onMoveEnd={(event, moved) => {
        // Only what the reader did: a fit reports itself with no event.
        if (event) {
          viewport.keep(moved);
        }
      }}
      nodeTypes={logicalNodeTypes}
      edgeTypes={logicalEdgeTypes}
      proOptions={{ hideAttribution: true }}
      nodesDraggable
      {...readOnlyKeyboard}
    >
      <Background />
      <GraphControls />
      <FitOnResize />
    </ReactFlow>
  );
}

function LogicalView({ data, isMobile }: { data: JGFGraph; isMobile: boolean }) {
  const graph = useMemo(() => networkGraphToReactFlow(data), [data]);
  const shape = useMemo(() => graphShape(graph), [graph]);

  // The same map the graph itself is coloured from — the legend built its own
  // before, which only agreed with the cards because both hashed the name.
  const legendColors = useMemo(() => stackColors(data), [data]);

  // Outside the key, so it survives the remount a changed shape forces.
  const viewport = useKeptViewport();

  if (Object.keys(data.nodes).length === 0) {
    return (
      <EmptyState
        message="No overlay networks found"
        icon={<Network className="mb-3 size-10 opacity-40" />}
      />
    );
  }

  return (
    <HighlightProvider edges={graph.edges}>
      <div
        className="relative"
        style={{
          height: isMobile ? "calc(100dvh - 3rem)" : "calc(100vh - 12rem)",
        }}
      >
        <ReactFlowProvider key={shape}>
          <LogicalCanvas
            graph={graph}
            viewport={viewport}
          />
        </ReactFlowProvider>
        <StackLegend
          key={isMobile ? "mobile" : "desktop"}
          colors={legendColors}
          isMobile={isMobile}
        />
      </div>
    </HighlightProvider>
  );
}

function PhysicalView({
  data,
  isMobile,
  network,
}: {
  data: JGFGraph;
  isMobile: boolean;
  network: string | null;
}) {
  const { nodes } = useMemo(() => placementGraphToReactFlow(data), [data]);
  const usage = usePlacementMetrics();
  const hasNodeExporter = isNodeExporterReady(useMonitoringStatus());
  const { getForNode } = useNodeMetrics({ usageOnly: true });

  const overlay = useMemo(
    () => ({ network, usage, nodeMetrics: hasNodeExporter ? getForNode : undefined }),
    [network, usage, hasNodeExporter, getForNode],
  );

  if (Object.values(data.nodes).every(({ metadata }) => metadata.kind !== "node")) {
    return (
      <EmptyState
        message="No nodes found in the cluster"
        icon={<Server className="mb-3 size-10 opacity-40" />}
      />
    );
  }

  return (
    <PhysicalViewContext.Provider value={overlay}>
      <div
        style={{
          height: isMobile ? "calc(100dvh - 3rem)" : "calc(100vh - 12rem)",
        }}
      >
        <ReactFlow
          aria-label="Task placement across cluster nodes"
          className="bg-background"
          nodes={nodes}
          edges={[]}
          nodeTypes={physicalNodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          proOptions={{ hideAttribution: true }}
          nodesDraggable
          {...readOnlyKeyboard}
        >
          <Background />
          <GraphControls />
          <FitOnResize />
        </ReactFlow>
      </div>
    </PhysicalViewContext.Provider>
  );
}

export default function Topology() {
  const isMobile = useMatchesBreakpoint("md", "below");
  const [viewParam, setView] = useUrlParam("view", "logical");
  const view: View = viewParam === "physical" ? "physical" : "logical";
  const [network, setNetwork] = useUrlParam("network");
  const queryClient = useQueryClient();

  const {
    data: topologyData,
    isLoading: loading,
    error: queryError,
  } = useQuery({
    queryKey: ["topology"],
    queryFn: () => api.topology(),
  });

  const networkData = topologyData?.graphs.find((graph) => graph.id === "network") ?? null;
  const placementData = topologyData?.graphs.find((graph) => graph.id === "placement") ?? null;
  const error = queryError ? getErrorMessage(queryError, "Failed to load topology") : null;
  const networks = useMemo(
    () => (placementData ? placementNetworks(placementData) : []),
    [placementData],
  );

  // A network that has since gone, or was never readable, selects nothing.
  const selectedNetwork = networks.some(({ id }) => id === network) ? network : null;

  useDebouncedInvalidation("/events", [["topology"]], 2_000);

  return (
    <div>
      <PageHeader title="Topology" />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <SegmentedControl
          segments={[
            { value: "logical" as const, label: "Logical" },
            { value: "physical" as const, label: "Physical" },
          ]}
          value={view}
          onChange={setView}
        />
        {view === "physical" && networks.length > 0 && (
          <select
            value={selectedNetwork ?? ""}
            onChange={({ target }) => setNetwork(target.value || null)}
            title="Emphasise the services on a network"
            aria-label="Network"
            className="h-8 rounded-md border bg-background px-2 text-xs"
          >
            <option value="">All networks</option>
            {networks.map(({ id, label }) => (
              <option
                key={id}
                value={id}
              >
                {label}
              </option>
            ))}
          </select>
        )}
      </div>

      {loading && <LoadingPage />}

      {error && (
        <div className="flex h-64 flex-col items-center justify-center gap-3">
          <p className="text-sm text-destructive">{error}</p>
          <button
            type="button"
            className="rounded-md bg-muted px-3 py-1.5 text-sm hover:bg-muted/80"
            onClick={() => {
              void queryClient.invalidateQueries({ queryKey: ["topology"] });
            }}
          >
            Retry
          </button>
        </div>
      )}

      <div className="rounded-lg ring-1 ring-border">
        {!loading &&
          !error &&
          view === "logical" &&
          (networkData ? (
            <LogicalView
              data={networkData}
              isMobile={isMobile}
            />
          ) : (
            <EmptyState message="Network topology unavailable" />
          ))}

        {!loading &&
          !error &&
          view === "physical" &&
          (placementData ? (
            <ReactFlowProvider>
              <PhysicalView
                data={placementData}
                isMobile={isMobile}
                network={selectedNetwork}
              />
            </ReactFlowProvider>
          ) : (
            <EmptyState message="Placement topology unavailable" />
          ))}
      </div>
    </div>
  );
}
