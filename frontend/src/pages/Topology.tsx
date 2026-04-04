import { api } from "../api/client";
import type { JGFGraph } from "../api/types";
import "@xyflow/react/dist/style.css";
import EmptyState from "../components/EmptyState";
import { LoadingPage } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import SegmentedControl from "../components/SegmentedControl";
import GroupNode from "../components/topology/GroupNode";
import { HighlightProvider } from "../components/topology/HighlightContext";
import NetworkEdge from "../components/topology/NetworkEdge";
import PhysicalNodeCard from "../components/topology/PhysicalNodeCard";
import ServiceCardNode from "../components/topology/ServiceCardNode";
import { useMatchesBreakpoint } from "../hooks/useMatchesBreakpoint";
import { useResourceStream } from "../hooks/useResourceStream";
import { computeLayout } from "../lib/layoutElk";
import {
  networkGraphToReactFlow,
  placementGraphToReactFlow,
  hashColor,
} from "../lib/topologyTransform";
import { getErrorMessage } from "../lib/utils";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ReactFlow, ReactFlowProvider, Background, type Node, type Edge } from "@xyflow/react";
import { Info, Network, Server, X } from "lucide-react";
import { useState, useEffect, useRef, useCallback, useMemo } from "react";

const logicalNodeTypes = {
  stackGroup: GroupNode,
  serviceCard: ServiceCardNode,
};
const logicalEdgeTypes = { networkEdge: NetworkEdge };
const physicalNodeTypes = { physicalNode: PhysicalNodeCard };

type View = "logical" | "physical";

function StackLegend({
  stackColors,
  isMobile,
}: {
  stackColors: Map<string, string>;
  isMobile: boolean;
}) {
  const [open, setOpen] = useState(!isMobile);

  if (stackColors.size === 0) {
    return null;
  }

  if (isMobile && !open) {
    return (
      <button
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
            onClick={() => setOpen(false)}
            className="ms-2 text-muted-foreground hover:text-foreground"
          >
            <X className="size-3" />
          </button>
        )}
      </div>
      <div className="flex flex-col gap-1">
        {[...stackColors.entries()].map(([stack, color]) => (
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

/**
 * Hook: run ELK layout async; only re-layout when graph structure changes.
 */
function useElkLayout(rawNodes: Node[], rawEdges: Edge[]) {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);
  const [ready, setReady] = useState(false);

  // Keep refs so layout effect always uses latest data
  const nodesRef = useRef(rawNodes);
  const edgesRef = useRef(rawEdges);
  nodesRef.current = rawNodes;
  edgesRef.current = rawEdges;

  // Structural fingerprint: only changes when nodes/edges are added/removed
  const structureKey = useMemo(() => {
    const nodeKey = rawNodes
      .map(({ id, parentId }) => `${id}:${parentId ?? ""}`)
      .sort()
      .join(",");
    const edgeKey = rawEdges
      .map(({ source, target }) => `${source}>${target}`)
      .sort()
      .join(",");

    return `${nodeKey}|${edgeKey}`;
  }, [rawNodes, rawEdges]);

  // Full re-layout only when structure changes
  useEffect(() => {
    let cancelled = false;
    computeLayout(nodesRef.current, edgesRef.current).then((result) => {
      if (!cancelled) {
        setNodes(result.nodes);
        setEdges(result.edges);
        setReady(true);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [structureKey]);

  // Patch node data in-place when only display data changes (replicas, status, etc.)
  useEffect(() => {
    if (!ready) {
      return;
    }

    const dataMap = new Map(rawNodes.map(({ id, data }) => [id, data]));

    setNodes((previous) =>
      previous.map((node) => {
        const data = dataMap.get(node.id);

        return data && data !== node.data ? { ...node, data } : node;
      }),
    );
  }, [rawNodes, ready]);

  return { nodes, edges, ready };
}

function LogicalView({ data, isMobile }: { data: JGFGraph; isMobile: boolean }) {
  const { nodes: rawNodes, edges: rawEdges } = useMemo(() => networkGraphToReactFlow(data), [data]);
  const { nodes, edges, ready } = useElkLayout(rawNodes, rawEdges);

  const stackColors = useMemo(() => {
    const map = new Map<string, string>();
    for (const hyperedge of data.hyperedges ?? []) {
      if (hyperedge.metadata.kind === "stack") {
        const name = hyperedge.metadata.name as string;
        if (!map.has(name)) {
          map.set(name, hashColor(name));
        }
      }
    }
    return map;
  }, [data]);

  if (Object.keys(data.nodes).length === 0) {
    return (
      <EmptyState
        message="No overlay networks found"
        icon={<Network className="mb-3 size-10 opacity-40" />}
      />
    );
  }

  if (!ready) {
    return null;
  }

  return (
    <HighlightProvider edges={rawEdges}>
      <div
        className="relative"
        style={{
          height: isMobile ? "calc(100dvh - 3rem)" : "calc(100vh - 12rem)",
        }}
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={logicalNodeTypes}
          edgeTypes={logicalEdgeTypes}
          fitView
          proOptions={{ hideAttribution: true }}
          nodesDraggable
          nodesConnectable={false}
        >
          <Background />
        </ReactFlow>
        <StackLegend
          key={isMobile ? "mobile" : "desktop"}
          stackColors={stackColors}
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
        nodes={nodes}
        edges={[]}
        nodeTypes={physicalNodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        proOptions={{ hideAttribution: true }}
        nodesDraggable
        nodesConnectable={false}
      >
        <Background />
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
    retry: 1,
  });

  const networkData = topologyData?.graphs.find((g) => g.id === "network") ?? null;
  const placementData = topologyData?.graphs.find((g) => g.id === "placement") ?? null;
  const error = queryError ? getErrorMessage(queryError, "Failed to load topology") : null;

  const refetchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const debouncedRefetch = useCallback(() => {
    if (refetchTimerRef.current) {
      clearTimeout(refetchTimerRef.current);
    }

    refetchTimerRef.current = setTimeout(() => {
      refetchTimerRef.current = null;
      void queryClient.invalidateQueries({ queryKey: ["topology"] });
    }, 2000);
  }, [queryClient]);

  useEffect(() => {
    return () => {
      if (refetchTimerRef.current) {
        clearTimeout(refetchTimerRef.current);
      }
    };
  }, []);

  useResourceStream(
    "/events",
    useCallback(() => {
      debouncedRefetch();
    }, [debouncedRefetch]),
  );

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
            className="rounded-md bg-muted px-3 py-1.5 text-sm hover:bg-muted/80"
            onClick={() => void queryClient.invalidateQueries({ queryKey: ["topology"] })}
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
            <ReactFlowProvider>
              <LogicalView
                data={networkData}
                isMobile={isMobile}
              />
            </ReactFlowProvider>
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
