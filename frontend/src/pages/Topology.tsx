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
import { buildLogicalFlow, buildPhysicalFlow, hashColor } from "@/lib/topologyTransform";
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

function StackLegend({ stackColors }: { stackColors: Map<string, string> }) {
  if (stackColors.size === 0) return null;

  return (
    <div className="absolute bottom-3 left-3 z-10 rounded-lg border bg-card/90 backdrop-blur-sm p-3 text-xs shadow-sm">
      <div className="font-medium text-muted-foreground mb-1.5">Stacks</div>
      <div className="flex flex-col gap-1">
        {[...stackColors.entries()].map(([stack, color]) => (
          <span key={stack} className="flex items-center gap-1.5">
            <span
              className="inline-block w-3 h-3 rounded-full shrink-0"
              style={{ backgroundColor: color }}
            />
            {stack}
          </span>
        ))}
      </div>
    </div>
  );
}

/** Hook: run ELK layout async; only re-layout when graph structure changes */
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
    const nk = rawNodes.map((n) => `${n.id}:${n.parentId ?? ""}`).sort().join(",");
    const ek = rawEdges.map((e) => `${e.source}>${e.target}`).sort().join(",");
    return `${nk}|${ek}`;
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
    return () => { cancelled = true; };
  }, [structureKey]);

  // Patch node data in-place when only display data changes (replicas, status, etc.)
  useEffect(() => {
    if (!ready) return;
    const dataMap = new Map(rawNodes.map((n) => [n.id, n.data]));
    setNodes((prev) =>
      prev.map((n) => {
        const d = dataMap.get(n.id);
        return d && d !== n.data ? { ...n, data: d } : n;
      }),
    );
  }, [rawNodes, ready]);

  return { nodes, edges, ready };
}

function LogicalView({ data }: { data: NetworkTopology }) {
  const { nodes: rawNodes, edges: rawEdges } = useMemo(() => buildLogicalFlow(data), [data]);
  const { nodes, edges, ready } = useElkLayout(rawNodes, rawEdges);

  const stackColors = useMemo(() => {
    const map = new Map<string, string>();
    for (const svc of data.nodes) {
      if (svc.stack && !map.has(svc.stack)) map.set(svc.stack, hashColor(svc.stack));
    }
    return map;
  }, [data]);

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
      <StackLegend stackColors={stackColors} />
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

      <div className="ring-1 ring-border rounded-lg">
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
    </div>
  );
}
