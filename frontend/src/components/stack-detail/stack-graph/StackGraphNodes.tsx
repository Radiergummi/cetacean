import { Detail, DetailList, Ports, useNodeFocus } from "@/components/graph/NodeChrome";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { MountNodeData, NetworkNodeData, ServiceNodeData, TaskCount } from "@/lib/stackGraph";
import { cn } from "@/lib/utils";
import type { NodeProps } from "@xyflow/react";
import { Network } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";

const nodeLink =
  "flex cursor-pointer items-center rounded-md border bg-card shadow-sm transition-colors " +
  "hover:border-ring hover:bg-accent focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none";

const chip = "max-w-44 gap-1.5 px-2 py-1 text-xs";

/** Short enough to keep the node three lines tall, which the layout depends on. */
function Running({ running, desired }: TaskCount) {
  return (
    <>
      <span
        data-healthy={running >= desired || undefined}
        className="text-status-warning data-healthy:text-status-ok"
      >
        {running}
      </span>
      /{desired} running
    </>
  );
}

export function NetworkNode({ id, data }: NodeProps & { data: NetworkNodeData }) {
  const focus = useNodeFocus(id);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Link
            to={data.href}
            aria-label={`Network ${data.name}`}
            {...focus}
            className={cn(
              nodeLink,
              chip,
              data.external && "border-dashed bg-transparent text-muted-foreground shadow-none",
              !data.referenced && "opacity-50",
            )}
          >
            <Ports />
            <Network className="size-3 shrink-0 text-teal-600 dark:text-teal-400" />
            <span className="truncate font-medium">{data.name}</span>
          </Link>
        }
      />
      <TooltipContent>
        <DetailList>
          <Detail term="Network">{data.name}</Detail>

          {data.driver && <Detail term="Driver">{data.driver}</Detail>}

          {data.scope && <Detail term="Scope">{data.scope}</Detail>}
        </DetailList>

        {data.external && (
          <p className="mt-1 text-muted-foreground">Attached, but not part of this stack.</p>
        )}

        {!data.referenced && <p className="mt-1 text-muted-foreground">No service attaches.</p>}
      </TooltipContent>
    </Tooltip>
  );
}

export function ServiceNode({ id, data }: NodeProps & { data: ServiceNodeData }) {
  const count = data.replicas ?? 0;
  const replicas =
    data.mode === "global" ? "global" : `${count} ${count === 1 ? "replica" : "replicas"}`;
  const focus = useNodeFocus(id);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Link
            to={data.href}
            aria-label={`Service ${data.name}`}
            {...focus}
            className={cn(
              nodeLink,
              "w-56 flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-sm",
            )}
          >
            <Ports />
            <span className="w-full truncate font-medium">{data.name}</span>
            <span className="w-full truncate font-mono text-xs text-muted-foreground">
              {data.image ?? "—"}
            </span>
            <span className="text-xs text-muted-foreground tabular-nums">
              {data.tasks ? <Running {...data.tasks} /> : replicas}
            </span>
          </Link>
        }
      />
      <TooltipContent>
        <DetailList>
          <Detail term="Service">{data.name}</Detail>

          {data.image && <Detail term="Image">{data.image}</Detail>}

          <Detail term="Mode">{data.mode}</Detail>

          {data.mode !== "global" && <Detail term="Replicas">{data.replicas ?? 0}</Detail>}

          {data.tasks && (
            <Detail term="Tasks">
              {data.tasks.running} of {data.tasks.desired} running
            </Detail>
          )}
        </DetailList>
      </TooltipContent>
    </Tooltip>
  );
}

/** Configs, secrets and volumes differ only in what they are called and drawn with. */
export function mountNodeType(kind: string, icon: ReactNode) {
  return function MountNode({ id, data }: NodeProps & { data: MountNodeData }) {
    const focus = useNodeFocus(id);

    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <Link
              to={data.href}
              aria-label={`${kind} ${data.name}`}
              {...focus}
              className={cn(nodeLink, chip, !data.referenced && "opacity-50")}
            >
              <Ports />
              {icon}
              <span className="truncate font-medium">{data.name}</span>
            </Link>
          }
        />
        <TooltipContent>
          <DetailList>
            <Detail term={kind}>{data.name}</Detail>

            {data.detail && <Detail term="Driver">{data.detail}</Detail>}
          </DetailList>

          {!data.referenced && (
            <p className="mt-1 text-muted-foreground">No service mounts this.</p>
          )}
        </TooltipContent>
      </Tooltip>
    );
  };
}
