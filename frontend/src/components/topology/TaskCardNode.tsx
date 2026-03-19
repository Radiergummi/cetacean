import { statusColor } from "../../lib/statusColor";
import { Handle, type NodeProps, Position } from "@xyflow/react";

type TaskCardData = {
  id: string;
  serviceId: string;
  serviceName: string;
  slot: number;
  state: string;
  image: string;
  highlighted: boolean;
  onHoverService: (serviceId: string | null) => void;
};

export default function TaskCardNode({ data }: NodeProps & { data: TaskCardData }) {
  return (
    <div
      data-highlighted={data.highlighted || undefined}
      className="w-48 rounded-lg border bg-card p-2 shadow-sm transition-shadow data-highlighted:ring-2 data-highlighted:ring-primary/50"
      onMouseEnter={() => data.onHoverService(data.serviceId)}
      onMouseLeave={() => data.onHoverService(null)}
    >
      <div
        className="truncate text-sm font-medium"
        title={`${data.serviceName}.${data.slot}`}
      >
        {data.serviceName}.{data.slot}
      </div>

      <div className="mt-0.5 flex items-center gap-1.5 text-xs">
        <span className={`inline-block size-2 rounded-full ${statusColor(data.state)}`} />
        <span>{data.state}</span>
      </div>

      <div
        className="mt-0.5 truncate text-xs text-muted-foreground"
        title={data.image}
      >
        {data.image}
      </div>

      <Handle
        type="target"
        position={Position.Left}
      />
      <Handle
        type="source"
        position={Position.Right}
      />
    </div>
  );
}
