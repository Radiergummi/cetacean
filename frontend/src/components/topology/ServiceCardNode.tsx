import { rolloutToneClass } from "../../lib/deriveServiceState";
import { replicaHealthColor } from "../../lib/statusColor";
import type { RolloutStatus } from "../../lib/topologyTransform";
import { cn } from "../../lib/utils";
import ResourceName from "../ResourceName";
import { useHighlight } from "./HighlightContext";
import { Handle, type NodeProps, Position } from "@xyflow/react";
import { useNavigate } from "react-router-dom";

type ServiceCardData = {
  id: string;
  name: string;
  mode: string;
  image: string;
  replicas: number;
  runningReplicas?: number | undefined;
  ports?: string[] | undefined;
  rollout?: RolloutStatus | undefined;
  stackColor?: string | undefined;
  hasSourceEdge?: boolean | undefined;
  hasTargetEdge?: boolean | undefined;
};

/**
 * The replica line on a card: how many tasks are up, and the tone that says
 * whether that is enough.
 *
 * A global service has no desired count — Docker does not publish one, and
 * `ReplicaCount` reports 0 — so it is described by what is running rather than
 * measured against a denominator that would read as 1/0.
 */
export function replicaStatus(
  mode: string,
  running: number,
  desired: number,
): { label: string; tone: string } {
  if (mode === "global") {
    return {
      label: `${running} running`,
      tone: running > 0 ? "bg-status-ok" : "bg-status-danger",
    };
  }

  return {
    label: `${running}/${desired}`,
    tone: replicaHealthColor(running, desired),
  };
}

export default function ServiceCardNode({ data }: NodeProps & { data: ServiceCardData }) {
  const navigate = useNavigate();
  const { hoveredId, neighbors, setHovered } = useHighlight();

  // No `?? data.replicas` fallback: the graph did not carry a running count at
  // all, so falling back to the desired one painted every service green.
  const { label, tone } = replicaStatus(data.mode, data.runningReplicas ?? 0, data.replicas);

  const dimmed = hoveredId != null && hoveredId !== data.id && !neighbors.has(data.id);

  return (
    <button
      type="button"
      data-dimmed={dimmed || undefined}
      aria-label={`Service ${data.name}`}
      className="block w-56 cursor-pointer rounded-lg bg-card p-3 text-left shadow-sm transition-all duration-200 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none data-dimmed:opacity-25 data-dimmed:grayscale-50"
      style={{
        borderWidth: 2,
        borderStyle: "solid",
        borderColor: data.stackColor ?? "var(--color-border)",
      }}
      onClick={() => navigate(`/services/${data.id}`)}
      onMouseEnter={() => setHovered(data.id)}
      onMouseLeave={() => setHovered(null)}
    >
      <div className="mb-1 flex items-center justify-between gap-1">
        <span
          className="truncate text-sm font-medium"
          title={data.name}
        >
          <ResourceName name={data.name} />
        </span>
        <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {data.mode === "global" ? "Global" : "Replicated"}
        </span>
      </div>

      <div
        className="mb-1 truncate text-xs text-muted-foreground"
        title={data.image}
      >
        {data.image}
      </div>

      <div className="mb-1 flex items-center gap-1.5 text-xs">
        <span className={`inline-block size-2 rounded-full ${tone}`} />
        <span>{label}</span>
      </div>

      {data.ports && data.ports.length > 0 && (
        <div className="space-y-0.5 text-xs text-muted-foreground">
          {data.ports.map((port) => (
            <div key={port}>{port}</div>
          ))}
        </div>
      )}

      {/* Only a rollout still in flight. `updateStatus` is "completed" on every
          service that has ever been updated, so rendering on its presence
          labelled the whole cluster "Updating…" forever. */}
      {data.rollout && (
        <div className={cn("mt-1 text-xs", rolloutToneClass(data.rollout.state))}>
          {data.rollout.label}
        </div>
      )}

      {data.hasTargetEdge && (
        <Handle
          type="target"
          position={Position.Left}
          className="size-0! border-0! bg-transparent!"
        />
      )}
      {data.hasSourceEdge && (
        <Handle
          type="source"
          position={Position.Right}
          className="size-0! border-0! bg-transparent!"
        />
      )}
    </button>
  );
}
