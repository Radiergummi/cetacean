import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import { ReactFlow, ReactFlowProvider, Background, type Node, type Edge } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { api } from "@/api/client";
import type { NetworkTopology, PlacementTopology } from "@/api/types";
import PageHeader from "@/components/PageHeader";
import EmptyState from "@/components/EmptyState";
import { LoadingPage } from "@/components/LoadingSkeleton";
import SegmentedControl from "@/components/SegmentedControl";
import { useSSE } from "@/hooks/useSSE";
import { computeLayout } from "@/lib/layoutElk";
import { buildLogicalFlow, buildPhysicalFlow } from "@/lib/topologyTransform";
import ServiceCardNode from "@/components/topology/ServiceCardNode";
import TaskCardNode from "@/components/topology/TaskCardNode";
import GroupNode from "@/components/topology/GroupNode";
import NetworkEdge from "@/components/topology/NetworkEdge";
import { Network, Server } from "lucide-react";

const logicalNodeTypes = {
  stackGroup: GroupNode,
  serviceCard: ServiceCardNode,
};
const logicalEdgeTypes = { networkEdge: NetworkEdge };
const physicalNodeTypes = { nodeGroup: GroupNode, taskCard: TaskCardNode };

type View = "logical" | "physical";

function NetworkLegend({ networks }: { networks: NetworkTopology["networks"] }) {
  if (networks.length === 0) return null;

  function hashColor(id: string): string {
    const COLORS = [
      "#3b82f6", "#ef4444", "#10b981", "#f59e0b", "#8b5cf6", "#ec4899",
      "#06b6d4", "#f97316", "#6366f1", "#14b8a6", "#e11d48", "#84cc16",
    ];
    let h = 0;
    for (let i = 0; i < id.length; i++) {
      h = (h * 31 + id.charCodeAt(i)) | 0;
    }
    return COLORS[Math.abs(h) % COLORS.length];
  }

  return (
    <div className="absolute bottom-3 left-3 z-10 rounded-lg border bg-card/90 backdrop-blur-sm p-3 text-xs shadow-sm">
      <div className="font-medium text-muted-foreground mb-1.5">Networks</div>
      <div className="flex flex-col gap-1">
        {networks.map((net) => (
          <span key={net.id} className="flex items-center gap-1.5">
            <span
              className="inline-block w-3 h-3 rounded-full shrink-0"
              style={{ backgroundColor: hashColor(net.id) }}
            />
            {net.name}
          </span>
        ))}
      </div>
    </div>
  );
}

/** Hook: run ELK layout async, return positioned nodes + edges */
function useElkLayout(rawNodes: Node[], rawEdges: Edge[]) {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [edges, setEdges] = useState<Edge[]>([]);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setReady(false);
    computeLayout(rawNodes, rawEdges).then((result) => {
      if (!cancelled) {
        setNodes(result.nodes);
        setEdges(result.edges);
        setReady(true);
      }
    });
    return () => { cancelled = true; };
  }, [rawNodes, rawEdges]);

  return { nodes, edges, ready };
}

function LogicalView({ data }: { data: NetworkTopology }) {
  const { nodes: rawNodes, edges: rawEdges } = useMemo(() => buildLogicalFlow(data), [data]);
  const { nodes, edges, ready } = useElkLayout(rawNodes, rawEdges);

  if (data.nodes.length === 0) {
    return (
      <EmptyState
        message="No overlay networks found"
        icon={<Network className="w-10 h-10 mb-3 opacity-40" />}
      />
    );
  }

  if (!ready) return null;

  return (
    <div className="relative" style={{ height: "calc(100vh - 12rem)" }}>
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
      <NetworkLegend networks={data.networks} />
    </div>
  );
}

function PhysicalView({ data }: { data: PlacementTopology }) {
  const [hoveredServiceId, setHoveredServiceId] = useState<string | null>(null);

  const onHoverService = useCallback(
    (serviceId: string | null) => setHoveredServiceId(serviceId),
    [],
  );

  const { nodes: rawNodes } = useMemo(() => buildPhysicalFlow(data), [data]);
  const emptyEdges = useMemo<Edge[]>(() => [], []);
  const { nodes: layoutNodes, ready } = useElkLayout(rawNodes, emptyEdges);

  const nodesWithHover = useMemo(
    () =>
      layoutNodes.map((n) => {
        if (n.type === "taskCard") {
          return {
            ...n,
            data: {
              ...n.data,
              highlighted: n.data.serviceId === hoveredServiceId,
              onHoverService,
            },
          };
        }
        return n;
      }),
    [layoutNodes, hoveredServiceId, onHoverService],
  );

  if (data.nodes.length === 0) {
    return (
      <EmptyState
        message="No nodes found in the cluster"
        icon={<Server className="w-10 h-10 mb-3 opacity-40" />}
      />
    );
  }

  if (!ready) return null;

  return (
    <div style={{ height: "calc(100vh - 12rem)" }}>
      <ReactFlow
        nodes={nodesWithHover}
        edges={[]}
        nodeTypes={physicalNodeTypes}
        fitView
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
  const [view, setView] = useState<View>("logical");
  const [networkData, setNetworkData] = useState<NetworkTopology | null>(null);
  const [placementData, setPlacementData] = useState<PlacementTopology | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const initialLoadRef = useRef(true);

  const fetchData = useCallback(async () => {
    if (initialLoadRef.current) {
      setLoading(true);
    }
    setError(null);
    try {
      const [net, place] = await Promise.all([api.topologyNetworks(), api.topologyPlacement()]);
      setNetworkData(net);
      setPlacementData(place);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load topology");
    } finally {
      setLoading(false);
      initialLoadRef.current = false;
    }
  }, []);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const refetchTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const debouncedRefetch = useCallback(() => {
    if (refetchTimerRef.current) return;
    refetchTimerRef.current = setTimeout(() => {
      refetchTimerRef.current = null;
      fetchData();
    }, 2000);
  }, [fetchData]);

  useEffect(() => {
    return () => {
      if (refetchTimerRef.current) clearTimeout(refetchTimerRef.current);
    };
  }, []);

  useSSE(
    ["service", "task", "node", "network"],
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
        <div className="flex flex-col items-center justify-center h-64 gap-3">
          <p className="text-sm text-destructive">{error}</p>
          <button
            className="text-sm px-3 py-1.5 rounded-md bg-muted hover:bg-muted/80"
            onClick={fetchData}
          >
            Retry
          </button>
        </div>
      )}

      {!loading && !error && view === "logical" && networkData && (
        <ReactFlowProvider>
          <LogicalView data={networkData} />
        </ReactFlowProvider>
      )}

      {!loading && !error && view === "physical" && placementData && (
        <ReactFlowProvider>
          <PhysicalView data={placementData} />
        </ReactFlowProvider>
      )}
    </div>
  );
}
