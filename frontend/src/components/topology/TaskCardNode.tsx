import { Handle, Position, type NodeProps } from "@xyflow/react";

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

function stateColor(state: string): string {
  switch (state) {
    case "running":
    case "complete":
      return "bg-green-500";
    case "failed":
    case "rejected":
      return "bg-red-500";
    case "preparing":
    case "starting":
    case "pending":
    case "assigned":
    case "accepted":
      return "bg-yellow-500";
    default:
      return "bg-gray-400";
  }
}

export default function TaskCardNode({ data }: NodeProps & { data: TaskCardData }) {
  return (
    <div
      data-highlighted={data.highlighted || undefined}
      className="w-48 border rounded-lg bg-card shadow-sm p-2 transition-shadow data-highlighted:ring-2 data-highlighted:ring-primary/50"
      onMouseEnter={() => data.onHoverService(data.serviceId)}
      onMouseLeave={() => data.onHoverService(null)}
    >
      <div className="text-sm font-medium truncate" title={`${data.serviceName}.${data.slot}`}>
        {data.serviceName}.{data.slot}
      </div>

      <div className="flex items-center gap-1.5 text-xs mt-0.5">
        <span className={`inline-block size-2 rounded-full ${stateColor(data.state)}`} />
        <span>{data.state}</span>
      </div>

      <div className="text-xs text-muted-foreground truncate mt-0.5" title={data.image}>
        {data.image}
      </div>

      <Handle type="target" position={Position.Left} />
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
