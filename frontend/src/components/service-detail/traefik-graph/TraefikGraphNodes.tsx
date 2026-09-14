import { Detail, DetailList, Ports, useNodeFocus } from "@/components/graph/NodeChrome";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { badgePurple, badgeTeal } from "@/lib/integrationLabels";
import {
  type EntrypointNodeData,
  type MiddlewareNodeData,
  type RouterNodeData,
  type ServiceNodeData,
} from "@/lib/traefikGraph";
import { cn } from "@/lib/utils";
import type { NodeProps } from "@xyflow/react";
import { Lock } from "lucide-react";

// A node reveals the rest of itself in a tooltip, which a pointer opens by
// hovering and a keyboard by focusing — hence a button around a node that does
// nothing when pressed. Entrypoints hide nothing, so they stay out of the way.
const focusRing = "focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none";

export function EntrypointNode({ data }: NodeProps & { data: EntrypointNodeData }) {
  return (
    <div className="flex max-w-28 items-center">
      <Ports />
      <span className={cn(badgeTeal, "max-w-full truncate")}>{data.name}</span>
    </div>
  );
}

export function RouterNode({ id, data }: NodeProps & { data: RouterNodeData }) {
  const { certResolver, domains, options } = data.tls ?? {};
  const priority = (data.priority ?? 0) > 0 ? data.priority : undefined;
  const focus = useNodeFocus(id);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-label={`Router ${data.name}`}
            {...focus}
            className={cn(
              "flex w-64 flex-col gap-1 rounded-lg border bg-card px-3 py-2 text-start text-sm shadow-sm",
              focusRing,
            )}
          >
            <Ports />
            <header className="flex items-center gap-2">
              <span className="truncate font-medium">{data.name}</span>

              {data.tls && <Lock className="size-3 shrink-0 text-status-ok" />}

              {priority != null && (
                <span className="ms-auto text-xs text-muted-foreground">{priority}</span>
              )}
            </header>

            {data.rule && (
              <code className="truncate rounded-md bg-muted px-1.5 py-1 font-mono text-xs text-muted-foreground">
                {data.rule}
              </code>
            )}
          </button>
        }
      />
      <TooltipContent>
        <DetailList>
          <Detail term="Router">{data.name}</Detail>

          {data.rule && <Detail term="Rule">{data.rule}</Detail>}

          {priority != null && <Detail term="Priority">{priority}</Detail>}

          {certResolver && <Detail term="Resolver">{certResolver}</Detail>}

          {options && <Detail term="TLS options">{options}</Detail>}

          {domains?.map((domain) => (
            <Detail
              key={domain.main}
              term="Domain"
            >
              {[domain.main, ...(domain.sans ?? [])].join(", ")}
            </Detail>
          ))}
        </DetailList>
      </TooltipContent>
    </Tooltip>
  );
}

export function MiddlewareNode({ id, data }: NodeProps & { data: MiddlewareNodeData }) {
  const config = Object.entries(data.config ?? {});
  const focus = useNodeFocus(id);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-label={`Middleware ${data.name}`}
            {...focus}
            className={cn("flex max-w-32 flex-col items-center gap-0.5 rounded-md", focusRing)}
          >
            <Ports />
            <span
              className={cn(
                badgePurple,
                "max-w-full truncate",
                data.external && "border border-dashed bg-transparent text-muted-foreground",
                !data.referenced && "opacity-50",
              )}
            >
              {data.name}
            </span>

            {data.type && (
              <span className="text-center text-[10px] text-muted-foreground">{data.type}</span>
            )}
          </button>
        }
      />
      <TooltipContent>
        <DetailList>
          <Detail term="Middleware">{data.name}</Detail>

          {data.type && <Detail term="Type">{data.type}</Detail>}

          {config.map(([key, value]) => (
            <Detail
              key={key}
              term={key}
            >
              {value}
            </Detail>
          ))}
        </DetailList>

        {data.external && (
          <p className="mt-1 text-muted-foreground">Defined outside this service.</p>
        )}

        {!data.referenced && <p className="mt-1 text-muted-foreground">No router uses this.</p>}
      </TooltipContent>
    </Tooltip>
  );
}

export function ServiceNode({ id, data }: NodeProps & { data: ServiceNodeData }) {
  const unresolved = data.origin === "unresolved";
  const focus = useNodeFocus(id);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-label={unresolved ? "Unresolved service" : `Service ${data.name}`}
            {...focus}
            className={cn(
              "flex w-50 flex-col gap-0.5 rounded-lg border bg-card px-3 py-2 text-start text-sm shadow-sm",
              focusRing,
              data.origin !== "declared" && "border-dashed bg-transparent shadow-none",
              !data.referenced && "opacity-50",
            )}
          >
            <Ports />
            <span
              className={cn("truncate font-medium", unresolved && "text-muted-foreground italic")}
            >
              {unresolved ? "Unresolved" : data.name}
            </span>

            <span className="truncate font-mono text-xs text-muted-foreground">
              {[data.scheme, data.port && `:${data.port}`].filter(Boolean).join("") || "—"}
            </span>
          </button>
        }
      />
      <TooltipContent>
        {unresolved ? (
          <p>
            Router <span className="font-mono">{data.name}</span> names no service, and these labels
            declare none it could bind to on its own.
          </p>
        ) : (
          <>
            <DetailList>
              <Detail term="Service">{data.name}</Detail>

              {data.port != null && <Detail term="Port">{data.port}</Detail>}

              {data.scheme && <Detail term="Scheme">{data.scheme}</Detail>}
            </DetailList>

            {data.implicit && (
              <p className="mt-1 text-muted-foreground">
                Bound implicitly — the router names no service.
              </p>
            )}

            {data.origin === "external" && (
              <p className="mt-1 text-muted-foreground">Defined outside this service.</p>
            )}

            {!data.referenced && (
              <p className="mt-1 text-muted-foreground">No router routes to this.</p>
            )}
          </>
        )}
      </TooltipContent>
    </Tooltip>
  );
}
