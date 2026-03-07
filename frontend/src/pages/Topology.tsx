import { useState, useEffect, useRef, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import * as d3Force from "d3-force";
import * as d3Zoom from "d3-zoom";
import * as d3Selection from "d3-selection";
import * as d3Drag from "d3-drag";
import { Network, Server } from "lucide-react";
import { api } from "@/api/client";
import type {
  NetworkTopology,
  PlacementTopology,
  TopoClusterNode,
} from "@/api/types";
import PageHeader from "@/components/PageHeader";
import EmptyState from "@/components/EmptyState";
import { LoadingPage } from "@/components/LoadingSkeleton";
import { useSSE } from "@/hooks/useSSE";

const STACK_COLORS = [
  "#3b82f6",
  "#ef4444",
  "#10b981",
  "#f59e0b",
  "#8b5cf6",
  "#ec4899",
  "#06b6d4",
  "#f97316",
  "#6366f1",
  "#14b8a6",
  "#e11d48",
  "#84cc16",
];

function hashString(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (Math.imul(31, h) + s.charCodeAt(i)) | 0;
  }
  return Math.abs(h);
}

function stackColor(stack: string | undefined): string {
  if (!stack) return "#6b7280";
  return STACK_COLORS[hashString(stack) % STACK_COLORS.length];
}

function serviceColor(serviceName: string): string {
  return STACK_COLORS[hashString(serviceName) % STACK_COLORS.length];
}

function taskStateColor(state: string): string {
  switch (state) {
    case "running":
      return "#22c55e";
    case "failed":
    case "rejected":
      return "#ef4444";
    case "preparing":
    case "starting":
    case "pending":
      return "#eab308";
    case "shutdown":
    case "complete":
    default:
      return "#9ca3af";
  }
}

type Tab = "network" | "placement";

interface SimNode extends d3Force.SimulationNodeDatum {
  id: string;
  name: string;
  stack?: string;
  replicas: number;
}

interface SimLink extends d3Force.SimulationLinkDatum<SimNode> {
  networks: string[];
}

interface Tooltip {
  x: number;
  y: number;
  content: React.ReactNode;
}

function useTooltip() {
  const [tooltip, setTooltip] = useState<Tooltip | null>(null);
  const show = useCallback((x: number, y: number, content: React.ReactNode) => {
    setTooltip({ x, y, content });
  }, []);
  const hide = useCallback(() => setTooltip(null), []);
  return { tooltip, show, hide };
}

function TooltipOverlay({ tooltip }: { tooltip: Tooltip | null }) {
  if (!tooltip) return null;
  return (
    <div
      className="pointer-events-none fixed z-50 rounded bg-popover border px-2.5 py-1.5 text-xs text-popover-foreground shadow-md"
      style={{ left: tooltip.x + 12, top: tooltip.y - 8 }}
    >
      {tooltip.content}
    </div>
  );
}

function NetworkView({ data }: { data: NetworkTopology }) {
  const svgRef = useRef<SVGSVGElement>(null);
  const navigate = useNavigate();
  const [positions, setPositions] = useState<Map<string, { x: number; y: number }>>(new Map());
  const [links, setLinks] = useState<Array<{ source: { x: number; y: number; id: string }; target: { x: number; y: number; id: string }; networks: string[] }>>([]);
  const simulationRef = useRef<d3Force.Simulation<SimNode, SimLink> | null>(null);
  const { tooltip, show, hide } = useTooltip();

  useEffect(() => {
    const svg = svgRef.current;
    if (!svg || data.nodes.length === 0) return;

    const rect = svg.getBoundingClientRect();
    const width = rect.width || 800;
    const height = rect.height || 600;

    const simNodes: SimNode[] = data.nodes.map((n) => ({ ...n }));
    const simLinks: SimLink[] = data.edges.map((e) => ({
      source: e.source,
      target: e.target,
      networks: e.networks,
    }));

    const simulation = d3Force
      .forceSimulation(simNodes)
      .force(
        "link",
        d3Force
          .forceLink<SimNode, SimLink>(simLinks)
          .id((d) => d.id)
          .distance(120),
      )
      .force("charge", d3Force.forceManyBody().strength(-200))
      .force("center", d3Force.forceCenter(width / 2, height / 2))
      .on("tick", () => {
        const pos = new Map<string, { x: number; y: number }>();
        for (const n of simNodes) {
          pos.set(n.id, { x: n.x ?? 0, y: n.y ?? 0 });
        }
        setPositions(new Map(pos));
        setLinks(
          simLinks.map((l) => ({
            source: l.source as unknown as { x: number; y: number; id: string },
            target: l.target as unknown as { x: number; y: number; id: string },
            networks: l.networks,
          })),
        );
      });

    simulationRef.current = simulation;

    // Zoom
    const sel = d3Selection.select(svg);
    const g = d3Selection.select(svg.querySelector("g")!);
    const zoom = d3Zoom
      .zoom<SVGSVGElement, unknown>()
      .scaleExtent([0.2, 4])
      .on("zoom", (event) => {
        g.attr("transform", event.transform.toString());
      });
    sel.call(zoom);

    // Drag
    const dragBehavior = d3Drag
      .drag<SVGCircleElement, SimNode>()
      .on("start", (event, d) => {
        if (!event.active) simulation.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
      })
      .on("drag", (event, d) => {
        d.fx = event.x;
        d.fy = event.y;
      })
      .on("end", (event, d) => {
        if (!event.active) simulation.alphaTarget(0);
        d.fx = null;
        d.fy = null;
      });

    // Bind drag to circles after a short delay so they render
    const timer = setTimeout(() => {
      const circles = d3Selection.select(svg).selectAll<SVGCircleElement, SimNode>("circle.node");
      circles.data(simNodes, (d) => d.id);
      circles.call(dragBehavior);
    }, 50);

    return () => {
      clearTimeout(timer);
      simulation.stop();
    };
  }, [data]);

  // Build network name lookup
  const networkNames = new Map(data.networks.map((n) => [n.id, n.name]));

  // Collect stack -> color mappings for the legend
  const stackNames = new Map<string, string>();
  for (const node of data.nodes) {
    if (node.stack) {
      stackNames.set(node.stack, stackColor(node.stack));
    }
  }
  const hasUnstacked = data.nodes.some((n) => !n.stack);

  if (data.nodes.length === 0) {
    return <EmptyState message="No overlay networks found" icon={<Network className="w-10 h-10 mb-3 opacity-40" />} />;
  }

  return (
    <div className="relative">
      <svg
        ref={svgRef}
        className="w-full border rounded-lg bg-muted/20"
        style={{ height: "calc(100vh - 16rem)" }}
        onMouseLeave={hide}
      >
        <g>
          {links.map((l, i) => (
            <g key={i}>
              <line
                x1={l.source.x}
                y1={l.source.y}
                x2={l.target.x}
                y2={l.target.y}
                stroke="currentColor"
                className="text-muted-foreground/40"
                strokeWidth={1.5}
              />
              <text
                x={(l.source.x + l.target.x) / 2}
                y={(l.source.y + l.target.y) / 2 - 6}
                textAnchor="middle"
                className="fill-muted-foreground text-[10px]"
              >
                {l.networks.map((id) => networkNames.get(id) || id.slice(0, 8)).join(", ")}
              </text>
            </g>
          ))}
          {data.nodes.map((node) => {
            const pos = positions.get(node.id);
            if (!pos) return null;
            const r = 8 + Math.sqrt(node.replicas) * 6;
            return (
              <g
                key={node.id}
                style={{ cursor: "pointer" }}
                onClick={() => navigate(`/services/${node.id}`)}
                onMouseMove={(e) =>
                  show(e.clientX, e.clientY, (
                    <div>
                      <div className="font-medium">{node.name}</div>
                      {node.stack && <div className="text-muted-foreground">Stack: {node.stack}</div>}
                      <div className="text-muted-foreground">Replicas: {node.replicas}</div>
                    </div>
                  ))
                }
                onMouseLeave={hide}
              >
                <circle
                  className="node"
                  cx={pos.x}
                  cy={pos.y}
                  r={r}
                  fill={stackColor(node.stack)}
                  stroke="white"
                  strokeWidth={2}
                  data-id={node.id}
                />
                <text
                  x={pos.x}
                  y={pos.y + r + 14}
                  textAnchor="middle"
                  className="fill-foreground text-xs"
                >
                  {node.name}
                </text>
              </g>
            );
          })}
        </g>
      </svg>
      <TooltipOverlay tooltip={tooltip} />
      {(stackNames.size > 0 || hasUnstacked) && (
        <div className="flex flex-wrap gap-3 p-3 mt-4 border rounded-lg bg-muted/20">
          <span className="text-xs font-medium text-muted-foreground mr-1">Stacks:</span>
          {Array.from(stackNames.entries()).map(([name, color]) => (
            <span key={name} className="flex items-center gap-1.5 text-xs">
              <span
                className="inline-block w-3 h-3 rounded-full shrink-0"
                style={{ backgroundColor: color }}
              />
              {name}
            </span>
          ))}
          {hasUnstacked && (
            <span className="flex items-center gap-1.5 text-xs">
              <span
                className="inline-block w-3 h-3 rounded-full shrink-0"
                style={{ backgroundColor: "#6b7280" }}
              />
              <span className="italic text-muted-foreground">no stack</span>
            </span>
          )}
        </div>
      )}
    </div>
  );
}

function PlacementView({ data }: { data: PlacementTopology }) {
  const navigate = useNavigate();

  if (data.nodes.length === 0) {
    return <EmptyState message="No nodes found in the cluster" icon={<Server className="w-10 h-10 mb-3 opacity-40" />} />;
  }

  // Collect all unique service names for the legend
  const serviceNames = new Map<string, string>();
  for (const node of data.nodes) {
    for (const task of node.tasks) {
      serviceNames.set(task.serviceName, task.serviceId);
    }
  }

  return (
    <div>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3 mb-6">
        {data.nodes.map((node) => (
          <PlacementNodeCard key={node.id} node={node} navigate={navigate} />
        ))}
      </div>
      {serviceNames.size > 0 && (
        <div className="flex flex-wrap gap-3 p-3 border rounded-lg bg-muted/20">
          <span className="text-xs font-medium text-muted-foreground mr-1">Services:</span>
          {Array.from(serviceNames.entries()).map(([name, id]) => (
            <button
              key={name}
              className="flex items-center gap-1.5 text-xs hover:underline"
              onClick={() => navigate(`/services/${id}`)}
            >
              <span
                className="inline-block w-3 h-3 rounded-full shrink-0"
                style={{ backgroundColor: serviceColor(name) }}
              />
              {name}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function PlacementNodeCard({
  node,
  navigate,
}: {
  node: TopoClusterNode;
  navigate: (path: string) => void;
}) {
  const { tooltip, show, hide } = useTooltip();
  const stateColor = node.state === "ready" ? "bg-green-500" : "bg-red-500";
  return (
    <div className="border rounded-lg overflow-hidden relative">
      <button
        className="w-full flex items-center gap-2 px-3 py-2 bg-muted/40 hover:bg-muted/60 transition-colors text-left"
        onClick={() => navigate(`/nodes/${node.id}`)}
      >
        <span className={`w-2 h-2 rounded-full ${stateColor}`} />
        <span className="font-medium text-sm truncate">{node.hostname}</span>
        <span className="ml-auto text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground">
          {node.role}
        </span>
      </button>
      <div className="p-3">
        {node.tasks.length === 0 ? (
          <p className="text-xs text-muted-foreground">No tasks</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {node.tasks.map((task) => (
              <div
                key={task.id}
                className="w-7 h-7 rounded-full flex items-center justify-center text-[9px] font-medium text-white cursor-default"
                style={{ backgroundColor: taskStateColor(task.state), borderColor: serviceColor(task.serviceName), borderWidth: 2 }}
                onMouseMove={(e) =>
                  show(e.clientX, e.clientY, (
                    <div>
                      <div className="font-medium">{task.serviceName}.{task.slot}</div>
                      <div className="text-muted-foreground">State: {task.state}</div>
                    </div>
                  ))
                }
                onMouseLeave={hide}
              >
                {task.slot}
              </div>
            ))}
          </div>
        )}
      </div>
      <TooltipOverlay tooltip={tooltip} />
    </div>
  );
}

export default function Topology() {
  const [tab, setTab] = useState<Tab>("network");
  const [networkData, setNetworkData] = useState<NetworkTopology | null>(null);
  const [placementData, setPlacementData] = useState<PlacementTopology | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    setLoading((prev) => prev || networkData === null);
    setError(null);
    try {
      const [net, place] = await Promise.all([
        api.topologyNetworks(),
        api.topologyPlacement(),
      ]);
      setNetworkData(net);
      setPlacementData(place);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load topology");
    } finally {
      setLoading(false);
    }
  }, [networkData]);

  useEffect(() => {
    fetchData();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // SSE: debounced refetch on cluster state changes
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
      <div className="flex items-center gap-1 mb-4">
        <button
          className={`text-sm px-3 py-1.5 rounded-md transition-colors ${
            tab === "network"
              ? "bg-muted text-foreground font-medium"
              : "text-muted-foreground hover:text-foreground hover:bg-muted/50"
          }`}
          onClick={() => setTab("network")}
        >
          Network
        </button>
        <button
          className={`text-sm px-3 py-1.5 rounded-md transition-colors ${
            tab === "placement"
              ? "bg-muted text-foreground font-medium"
              : "text-muted-foreground hover:text-foreground hover:bg-muted/50"
          }`}
          onClick={() => setTab("placement")}
        >
          Placement
        </button>
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

      {!loading && !error && tab === "network" && networkData && (
        <NetworkView data={networkData} />
      )}

      {!loading && !error && tab === "placement" && placementData && (
        <PlacementView data={placementData} />
      )}
    </div>
  );
}
