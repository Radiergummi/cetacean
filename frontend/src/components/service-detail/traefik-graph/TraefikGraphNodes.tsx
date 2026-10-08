import { Detail, DetailList, NodeDetail, Ports } from "@/components/graph/NodeChrome";
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
import { memo } from "react";

// A node reveals the rest of itself in a tooltip, which a pointer opens by
// hovering and a keyboard by focusing. Entrypoints hide nothing, so they stay
// out of the way. Memoised: the hover dims by relabelling every node, which
// leaves the data each one draws from untouched.

// Nothing points at it, said without the opacity a fade would cost the text.
const unreferenced = "bg-muted text-muted-foreground";

export const EntrypointNode = memo(function EntrypointNode({
  data,
}: NodeProps & { data: EntrypointNodeData }) {
  return (
    <div className="flex max-w-28 items-center">
      <Ports />
      <span className={cn(badgeTeal, "max-w-full truncate")}>{data.name}</span>
    </div>
  );
});

export const RouterNode = memo(function RouterNode({ data }: NodeProps & { data: RouterNodeData }) {
  const { certResolver, domains, options } = data.tls ?? {};
  const priority = (data.priority ?? 0) > 0 ? data.priority : undefined;

  return (
    <NodeDetail
      // The rule is the router, and React Flow's wrapper is a `role="application"`
      // a screen reader can only tab through, so the name has to carry it.
      label={["Router " + data.name, data.rule].filter(Boolean).join(", ")}
      className="flex w-64 flex-col gap-1 rounded-lg border bg-card px-3 py-2 text-start text-sm shadow-sm"
      face={
        <>
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
        </>
      }
    >
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
    </NodeDetail>
  );
});

export const MiddlewareNode = memo(function MiddlewareNode({
  data,
}: NodeProps & { data: MiddlewareNodeData }) {
  const config = Object.entries(data.config ?? {});

  return (
    <NodeDetail
      label={["Middleware " + data.name, data.type === data.name ? "" : data.type]
        .filter(Boolean)
        .join(", ")}
      className="flex max-w-32 flex-col items-center gap-0.5 rounded-md"
      face={
        <>
          <span
            className={cn(
              badgePurple,
              "max-w-full truncate",
              data.external && "border border-dashed bg-transparent text-muted-foreground",
              !data.referenced && unreferenced,
            )}
          >
            {data.name}
          </span>

          {data.type && (
            <span className="text-center text-xs text-muted-foreground">{data.type}</span>
          )}
        </>
      }
    >
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

      {data.external && <p className="mt-1 text-muted-foreground">Defined outside this service.</p>}

      {!data.referenced && <p className="mt-1 text-muted-foreground">No router uses this.</p>}
    </NodeDetail>
  );
});

export const ServiceNode = memo(function ServiceNode({
  data,
}: NodeProps & { data: ServiceNodeData }) {
  const unresolved = data.origin === "unresolved";
  const target = [data.scheme, data.port && `:${data.port}`].filter(Boolean).join("");

  return (
    <NodeDetail
      label={
        unresolved
          ? "Unresolved service"
          : ["Service " + data.name, target].filter(Boolean).join(", ")
      }
      className={cn(
        "flex w-50 flex-col gap-0.5 rounded-lg border bg-card px-3 py-2 text-start text-sm shadow-sm",
        data.origin !== "declared" && "border-dashed bg-transparent shadow-none",
        !data.referenced && "text-muted-foreground shadow-none",
      )}
      face={
        <>
          <span
            className={cn("truncate font-medium", unresolved && "text-muted-foreground italic")}
          >
            {unresolved ? "Unresolved" : data.name}
          </span>

          <span className="truncate font-mono text-xs text-muted-foreground">{target || "—"}</span>
        </>
      }
    >
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
    </NodeDetail>
  );
});
