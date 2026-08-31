import type { TopologyGraphData, TopologyNode, TopologyView } from "./layout";
import { toFlow, topologyViews } from "./layout";
import { Background, Controls, Handle, Position, ReactFlow, type NodeProps } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useMemo } from "react";

const viewLabels: Record<TopologyView, string> = {
  network: "Network",
  placement: "Placement",
};

const viewCaptions: Record<TopologyView, string> = {
  network: "Services and the overlay networks they attach to.",
  placement: "Cluster nodes and the services they run.",
};

/**
 * The accent each vertex kind carries. Colours come from the host's style
 * variables, so a widget matches the client it renders in rather than
 * insisting on Cetacean's palette.
 */
const typeAccent: Record<string, string> = {
  service: "border-l-primary",
  network: "border-l-chart-3",
  node: "border-l-chart-5",
};

/**
 * A state that means something is wrong, and should read that way at a glance.
 * Anything else — running, ready, an overlay's scope — is unremarkable.
 */
const alarmingStates = new Set(["failed", "pending", "down", "disconnected", "unknown"]);

function TopologyNodeCard({ data }: NodeProps) {
  const node = data as unknown as TopologyNode;
  const accent = typeAccent[node.type] ?? "border-l-muted-foreground";

  return (
    <div
      className={`min-w-40 rounded-md border border-l-4 bg-card px-3 py-2 text-left shadow-sm ${accent}`}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="!bg-muted-foreground"
      />

      <div className="truncate text-sm font-medium text-foreground">{node.label}</div>

      {node.detail && <div className="truncate text-xs text-muted-foreground">{node.detail}</div>}

      <div className="mt-1 flex items-center gap-1.5">
        <span className="text-[10px] tracking-wide text-muted-foreground uppercase">
          {node.type}
        </span>

        {node.state && (
          <span
            className={`text-[10px] font-medium ${
              alarmingStates.has(node.state) ? "text-destructive" : "text-muted-foreground"
            }`}
          >
            {node.state}
          </span>
        )}

        {node.group && (
          <span className="truncate text-[10px] text-muted-foreground">{node.group}</span>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Right}
        className="!bg-muted-foreground"
      />
    </div>
  );
}

const nodeTypes = { topologyNode: TopologyNodeCard };

/**
 * Renders a cluster topology graph with a switcher between its two views.
 *
 * Purely presentational: it holds no view state and fetches nothing, so the
 * whole of it is exercisable from a test with a literal graph. TopologyWidget
 * is the half that talks to the host.
 */
export function TopologyGraph({
  graph,
  onViewChange,
}: {
  graph: TopologyGraphData;
  onViewChange: (view: TopologyView) => void;
}) {
  const { edges, nodes } = useMemo(() => toFlow(graph), [graph]);
  const current = graph.view as TopologyView;

  return (
    <div className="flex h-full w-full flex-col bg-background">
      <div className="flex items-center justify-between gap-3 border-b px-3 py-2">
        <p className="truncate text-xs text-muted-foreground">{viewCaptions[current] ?? ""}</p>

        <div className="inline-flex shrink-0 items-center gap-0.5 rounded-md bg-card p-0.5 ring-1 ring-input ring-inset">
          {topologyViews.map((view) => (
            <button
              key={view}
              type="button"
              aria-pressed={view === current}
              onClick={() => {
                if (view !== current) {
                  onViewChange(view);
                }
              }}
              className="cursor-pointer rounded-sm px-3 py-1 text-sm font-medium text-muted-foreground transition hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground aria-pressed:shadow-sm"
            >
              {viewLabels[view]}
            </button>
          ))}
        </div>
      </div>

      {nodes.length === 0 ? (
        <p className="flex flex-1 items-center justify-center p-6 text-sm text-muted-foreground">
          Nothing to show in this view.
        </p>
      ) : (
        <div className="min-h-64 flex-1">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            nodesDraggable
            nodesConnectable={false}
            fitView
            proOptions={{ hideAttribution: false }}
          >
            <Background />
            <Controls showInteractive={false} />
          </ReactFlow>
        </div>
      )}
    </div>
  );
}
