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
};

export default function ServiceCardNode({ data }: NodeProps & { data: ServiceCardData }) {
  const navigate = useNavigate();
  const running = data.runningReplicas ?? data.replicas;

  const statusColor =
    running === data.replicas ? "bg-green-500" : running > 0 ? "bg-yellow-500" : "bg-red-500";

  return (
    <div
      className="w-56 border rounded-lg bg-card shadow-sm p-3 cursor-pointer hover:border-primary/50 transition-colors"
      onClick={() => navigate(`/services/${data.id}`)}
    >
      <div className="flex items-center justify-between gap-1 mb-1">
        <span className="font-medium text-sm truncate" title={data.name}>
          {data.name}
        </span>
        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-muted text-muted-foreground shrink-0">
          {data.mode === "global" ? "Global" : "Replicated"}
        </span>
      </div>

      <div className="text-xs text-muted-foreground truncate mb-1" title={data.image}>
        {data.image}
      </div>

      <div className="flex items-center gap-1.5 text-xs mb-1">
        <span className={`inline-block w-2 h-2 rounded-full ${statusColor}`} />
        <span>
          {running}/{data.replicas}
        </span>
      </div>

      {data.ports && data.ports.length > 0 && (
        <div className="text-xs text-muted-foreground space-y-0.5">
          {data.ports.map((p) => (
            <div key={p}>{p}</div>
          ))}
        </div>
      )}

      {data.updateStatus && <div className="text-xs text-yellow-500 mt-1">Updating...</div>}

      <Handle type="target" position={Position.Left} />
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
