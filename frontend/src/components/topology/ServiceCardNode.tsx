import ResourceName from "../ResourceName";
import { useHighlight } from "./HighlightContext";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { useNavigate } from "react-router-dom";

type ServiceCardData = {
  id: string;
  name: string;
  mode: string;
  image: string;
  replicas: number;
  runningReplicas?: number;
  ports?: string[];
  updateStatus?: string;
  stackColor?: string;
  hasSourceEdge?: boolean;
  hasTargetEdge?: boolean;
};

export default function ServiceCardNode({ data }: NodeProps & { data: ServiceCardData }) {
  const navigate = useNavigate();
  const { hoveredId, neighbors, setHovered } = useHighlight();
  const running = data.runningReplicas ?? data.replicas;

  const statusColor =
    running === data.replicas ? "bg-green-500" : running > 0 ? "bg-yellow-500" : "bg-red-500";

  const dimmed = hoveredId != null && hoveredId !== data.id && !neighbors.has(data.id);

  return (
    <div
      data-dimmed={dimmed || undefined}
      className="w-56 cursor-pointer rounded-lg bg-card p-3 shadow-sm transition-all duration-200 data-dimmed:opacity-25 data-dimmed:grayscale-50"
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
        <span className={`inline-block size-2 rounded-full ${statusColor}`} />
        <span>
          {running}/{data.replicas}
        </span>
      </div>

      {data.ports && data.ports.length > 0 && (
        <div className="space-y-0.5 text-xs text-muted-foreground">
          {data.ports.map((p) => (
            <div key={p}>{p}</div>
          ))}
        </div>
      )}

      {data.updateStatus && <div className="mt-1 text-xs text-yellow-500">Updating...</div>}

      {data.hasTargetEdge && (
        <Handle
          type="target"
          position={Position.Left}
          className="!h-0 !w-0 !border-0 !bg-transparent"
        />
      )}
      {data.hasSourceEdge && (
        <Handle
          type="source"
          position={Position.Right}
          className="!h-0 !w-0 !border-0 !bg-transparent"
        />
      )}
    </div>
  );
}
