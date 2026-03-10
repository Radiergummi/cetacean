import type { NodeProps } from "@xyflow/react";
import { useNavigate } from "react-router-dom";
import ResourceName from "../ResourceName";

type ServiceSummary = {
  serviceId: string;
  serviceName: string;
  image: string;
  running: number;
  total: number;
  states: string[];
};

export type PhysicalNodeData = {
  label: string;
  role: string;
  state: string;
  availability: string;
  services: ServiceSummary[];
};

function stateColor(running: number, total: number): string {
  if (running === total) return "bg-green-500";
  if (running > 0) return "bg-yellow-500";
  return "bg-red-500";
}

export default function PhysicalNodeCard({ data }: NodeProps & { data: PhysicalNodeData }) {
  const navigate = useNavigate();

  return (
    <div
      className="rounded-xl bg-muted/10 p-4"
      style={{
        borderWidth: 1,
        borderStyle: "dashed",
        borderColor: "var(--color-border)",
      }}
    >
      <div className="flex items-center gap-2 mb-3">
        <span className="text-sm font-semibold text-muted-foreground">
          {data.label}
        </span>
        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-muted text-muted-foreground">
          {data.role === "manager" ? "Manager" : "Worker"}
        </span>
        <span
          className={`inline-block w-2 h-2 rounded-full ${data.state === "ready" ? "bg-green-500" : "bg-red-500"}`}
        />
        {data.availability !== "active" && (
          <span className="text-[10px] text-muted-foreground">{data.availability}</span>
        )}
      </div>

      {data.services.length === 0 ? (
        <div className="text-xs text-muted-foreground">No running tasks</div>
      ) : (
        <div className="grid grid-cols-3 gap-2">
          {data.services.map((svc) => (
            <div
              key={svc.serviceId}
              className="border rounded-lg bg-card shadow-sm p-2.5 cursor-pointer hover:shadow-md transition-shadow"
              onClick={() => navigate(`/services/${svc.serviceId}`)}
            >
              <div className="font-medium text-xs truncate mb-0.5" title={svc.serviceName}>
                <ResourceName name={svc.serviceName} />
              </div>
              <div className="text-[11px] text-muted-foreground truncate mb-1" title={svc.image}>
                {svc.image}
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <span className={`inline-block w-2 h-2 rounded-full ${stateColor(svc.running, svc.total)}`} />
                <span>{svc.running}/{svc.total} tasks</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
