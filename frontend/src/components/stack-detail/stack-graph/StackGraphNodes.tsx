import { Detail, DetailList, NodeDetail } from "@/components/graph/NodeChrome";
import { TaskHealth } from "@/components/HealthIndicator";
import type { MountNodeData, NetworkNodeData, ServiceNodeData } from "@/lib/stackGraph";
import { cn } from "@/lib/utils";
import type { NodeProps } from "@xyflow/react";
import { Network } from "lucide-react";
import { memo, type ReactNode } from "react";

const nodeLink = cn(
  "flex cursor-pointer items-center rounded-md border bg-card shadow-sm transition-colors",
  "hover:border-ring hover:bg-accent",
);

const chip = "max-w-44 gap-1.5 px-2 py-1 text-xs";

// Nothing points at it, said by dropping the weight rather than the opacity a
// fade would cost: the name still has to be readable against the card.
const unreferenced = "text-muted-foreground shadow-none";

function PerService({ title, rows }: { title: string; rows: { service: string; of: string }[] }) {
  if (rows.length === 0) {
    return null;
  }

  return (
    <>
      <p className="mt-1.5 mb-0.5 text-muted-foreground">{title}</p>

      <DetailList>
        {rows.map(({ service, of }) => (
          <Detail
            key={service}
            term={service}
          >
            {of}
          </Detail>
        ))}
      </DetailList>
    </>
  );
}

// Memoised because the hover dims by relabelling every node: the data is what
// decides what a node draws, and that is untouched by a pointer crossing it.

export const NetworkNode = memo(function NetworkNode({
  data,
}: NodeProps & { data: NetworkNodeData }) {
  return (
    <NodeDetail
      href={data.href}
      label={`Network ${data.name}`}
      className={cn(
        nodeLink,
        chip,
        data.external && "border-dashed bg-transparent text-muted-foreground shadow-none",
        !data.referenced && unreferenced,
      )}
      face={
        <>
          <Network className="size-3 shrink-0 text-teal-600 dark:text-teal-400" />
          <span className="truncate font-medium">{data.name}</span>
        </>
      }
    >
      <DetailList>
        <Detail term="Network">{data.name}</Detail>

        {data.driver && <Detail term="Driver">{data.driver}</Detail>}

        {data.scope && <Detail term="Scope">{data.scope}</Detail>}
      </DetailList>

      <PerService
        title="Answers to"
        rows={data.aliases.map(({ service, names }) => ({ service, of: names.join(", ") }))}
      />

      {data.external && (
        <p className="mt-1 text-muted-foreground">Attached, but not part of this stack.</p>
      )}

      {!data.referenced && <p className="mt-1 text-muted-foreground">No service attaches.</p>}
    </NodeDetail>
  );
});

export const ServiceNode = memo(function ServiceNode({
  data,
}: NodeProps & { data: ServiceNodeData }) {
  const count = data.replicas ?? 0;
  const replicas =
    data.mode === "global" ? "global" : `${count} ${count === 1 ? "replica" : "replicas"}`;

  return (
    <NodeDetail
      href={data.href}
      label={`Service ${data.name}`}
      className={cn(nodeLink, "w-56 flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-sm")}
      face={
        <>
          <span className="w-full truncate font-medium">{data.name}</span>
          <span className="w-full truncate font-mono text-xs text-muted-foreground">
            {data.image ?? "—"}
          </span>
          <span className="text-xs text-muted-foreground tabular-nums">
            {data.tasks ? (
              <>
                <TaskHealth {...data.tasks} />/{data.tasks.desired} running
              </>
            ) : (
              replicas
            )}
          </span>
        </>
      }
    >
      <DetailList>
        <Detail term="Service">{data.name}</Detail>

        {data.image && <Detail term="Image">{data.image}</Detail>}

        <Detail term="Mode">{data.mode}</Detail>

        {data.mode !== "global" && <Detail term="Replicas">{data.replicas ?? 0}</Detail>}
      </DetailList>
    </NodeDetail>
  );
});

/** Configs, secrets and volumes differ only in what they are called and drawn with. */
export function mountNodeType(kind: string, icon: ReactNode) {
  return memo(function MountNode({ data }: NodeProps & { data: MountNodeData }) {
    return (
      <NodeDetail
        href={data.href}
        label={`${kind} ${data.name}`}
        className={cn(nodeLink, chip, !data.referenced && unreferenced)}
        face={
          <>
            {icon}
            <span className="truncate font-medium">{data.name}</span>
          </>
        }
      >
        <DetailList>
          <Detail term={kind}>{data.name}</Detail>

          {data.detail && <Detail term="Driver">{data.detail}</Detail>}
        </DetailList>

        <PerService
          title="Mounted at"
          rows={data.mountedBy.map(({ service, path }) => ({ service, of: path }))}
        />

        {!data.referenced && <p className="mt-1 text-muted-foreground">No service mounts this.</p>}
      </NodeDetail>
    );
  });
}
