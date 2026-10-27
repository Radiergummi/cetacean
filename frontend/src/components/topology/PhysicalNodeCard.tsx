import ResourceName from "../ResourceName";
import { usePhysicalView } from "./PhysicalViewContext";
import { colorForValue } from "@/components/metrics/ResourceGauge";
import type { NodeMetrics } from "@/hooks/useNodeMetrics";
import { placementUsageKey, type PlacementUsage } from "@/hooks/usePlacementMetrics";
import { formatBytes, formatPercentage } from "@/lib/format";
import { replicaHealthColor } from "@/lib/statusColor";
import { dimmedByNetwork, urnToId } from "@/lib/topologyTransform";
import type { NodeProps } from "@xyflow/react";
import { useNavigate } from "react-router-dom";

type ServiceSummary = {
  serviceId: string;
  serviceName: string;
  image: string;
  running: number;
  total: number;
  states: string[];
  networks: string[];
};

export type PhysicalNodeData = {
  label: string;
  address: string | undefined;
  role: string;
  state: string;
  availability: string;
  services: ServiceSummary[];
};

function NodeUsage({ metrics }: { metrics: NodeMetrics }) {
  const figures = [
    ["CPU", metrics.cpu],
    ["Mem", metrics.memory],
  ] as const;

  if (figures.every(([, value]) => value == null)) {
    return null;
  }

  return (
    <span className="ms-auto flex shrink-0 gap-2 text-[10px] whitespace-nowrap tabular-nums">
      {figures.map(([label, value]) => (
        <span
          key={label}
          style={value != null ? { color: colorForValue(value) } : undefined}
          className={value == null ? "text-muted-foreground" : undefined}
        >
          {label} {value != null ? formatPercentage(value, 0) : "—"}
        </span>
      ))}
    </span>
  );
}

function ServiceUsage({ usage }: { usage: PlacementUsage }) {
  const figures = [
    usage.cpu != null ? formatPercentage(usage.cpu, 1) : null,
    usage.memory != null ? formatBytes(usage.memory) : null,
  ].filter((figure) => figure !== null);

  if (figures.length === 0) {
    return null;
  }

  const text = figures.join(" · ");

  return (
    <span
      className="ms-auto min-w-0 truncate text-[11px] whitespace-nowrap text-muted-foreground tabular-nums"
      title={text}
    >
      {text}
    </span>
  );
}

export default function PhysicalNodeCard({ id, data }: NodeProps & { data: PhysicalNodeData }) {
  const navigate = useNavigate();
  const { network, usage, nodeMetrics } = usePhysicalView();
  const nodeId = urnToId(id);

  return (
    <div
      className="rounded-xl bg-muted/10 p-4"
      style={{
        borderWidth: 1,
        borderStyle: "dashed",
        borderColor: "var(--color-border)",
      }}
    >
      <div className="mb-3 flex items-center gap-2">
        <span className="text-sm font-semibold text-muted-foreground">{data.label}</span>
        <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
          {data.role === "manager" ? "Manager" : "Worker"}
        </span>
        <span
          data-ready={data.state === "ready" || undefined}
          className="inline-block size-2 rounded-full bg-status-danger data-ready:bg-status-ok"
        />
        {data.availability !== "active" && (
          <span className="text-[10px] text-muted-foreground">{data.availability}</span>
        )}
        {nodeMetrics && <NodeUsage metrics={nodeMetrics(data.label, data.address)} />}
      </div>

      {data.services.length === 0 ? (
        <div className="text-xs text-muted-foreground">No running tasks</div>
      ) : (
        <div className="grid grid-cols-3 gap-2">
          {data.services.map((svc) => {
            const serviceUsage = usage[placementUsageKey(svc.serviceId, nodeId)];

            return (
              <button
                key={svc.serviceId}
                type="button"
                aria-label={`Service ${svc.serviceName}`}
                data-dimmed={dimmedByNetwork(svc.networks, network) || undefined}
                className="cursor-pointer rounded-lg border bg-card p-2.5 text-left shadow-sm transition-[box-shadow,opacity] hover:shadow-md focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none data-dimmed:opacity-30"
                onClick={() => navigate(`/services/${svc.serviceId}`)}
              >
                <div
                  className="mb-0.5 truncate text-xs font-medium"
                  title={svc.serviceName}
                >
                  <ResourceName name={svc.serviceName} />
                </div>
                <div
                  className="mb-1 truncate text-[11px] text-muted-foreground"
                  title={svc.image}
                >
                  {svc.image}
                </div>
                <div className="flex items-center gap-1.5 text-xs">
                  <span
                    className={`inline-block size-2 rounded-full ${replicaHealthColor(svc.running, svc.total)}`}
                  />
                  <span className="shrink-0 whitespace-nowrap">
                    {svc.running}/{svc.total} tasks
                  </span>
                  {serviceUsage && <ServiceUsage usage={serviceUsage} />}
                </div>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
