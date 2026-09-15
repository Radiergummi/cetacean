import { api } from "../api/client";
import type { JGFGraph } from "../api/types";
import EmptyState from "../components/EmptyState";
import { FitOnResize, glide, GraphControls, readOnlyKeyboard } from "../components/graph/viewport";
import "@xyflow/react/dist/style.css";
import { LoadingPage } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import SegmentedControl from "../components/SegmentedControl";
import GroupNode from "../components/topology/GroupNode";
import { HighlightProvider } from "../components/topology/HighlightContext";
import NetworkEdge from "../components/topology/NetworkEdge";
import PhysicalNodeCard from "../components/topology/PhysicalNodeCard";
import ServiceCardNode from "../components/topology/ServiceCardNode";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { useMatchesBreakpoint } from "../hooks/useMatchesBreakpoint";
import { computeLayout } from "../lib/layoutElk";
import {
  networkGraphToReactFlow,
  placementGraphToReactFlow,
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

function LogicalCanvas({ graph }: { graph: Graph }) {
  const { nodes, edges, onNodesChange, bounds } = useMeasuredLayout(graph, computeLayout);
  const { fitView } = useReactFlow();

  useEffect(() => {
    if (bounds) {
      void fitView(glide());
    }
  }, [bounds, fitView]);

  return (
    <ReactFlow
      aria-label="Cluster network topology"
      className="bg-background transition-opacity duration-200"
      style={{ opacity: bounds ? 1 : 0 }}
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
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
          <LogicalCanvas graph={graph} />
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

function PhysicalView({ data, isMobile }: { data: JGFGraph; isMobile: boolean }) {
  const { nodes } = useMemo(() => placementGraphToReactFlow(data), [data]);

  if (Object.values(data.nodes).every(({ metadata }) => metadata.kind !== "node")) {
    return (
      <EmptyState
        message="No nodes found in the cluster"
        icon={<Server className="mb-3 size-10 opacity-40" />}
      />
    );
  }

  return (
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
  );
}

export default function Topology() {
  const isMobile = useMatchesBreakpoint("md", "below");
  const [view, setView] = useState<View>("logical");
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

  useDebouncedInvalidation("/events", [["topology"]], 2_000);

  return (
    <div>
      <PageHeader title="Topology" />
      <div className="mb-4">
        <SegmentedControl
          segments={[
            { value: "logical" as const, label: "Logical" },
            { value: "physical" as const, label: "Physical" },
          ]}
          value={view}
          onChange={setView}
        />
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
              />
            </ReactFlowProvider>
          ) : (
            <EmptyState message="Placement topology unavailable" />
          ))}
      </div>
    </div>
  );
}
