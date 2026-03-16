import type { NodeProps } from "@xyflow/react";

type GroupData = {
  label: string;
  role?: string;
  state?: string;
  availability?: string;
  variant: "stack" | "node";
  color?: string;
};

export default function GroupNode({ data }: NodeProps & { data: GroupData }) {
  const isNode = data.variant === "node";

  return (
    <div
      className="h-full w-full rounded-xl bg-muted/10 p-4"
      style={{
        borderWidth: 1,
        borderStyle: "dashed",
        borderColor: data.color ?? "var(--color-border)",
      }}
    >
      <div className="mb-2 flex items-center gap-2">
        <span
          className="text-sm font-semibold"
          style={{ color: data.color ?? "var(--color-muted-foreground)" }}
        >
          {data.label}
        </span>

        {isNode && data.role && (
          <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
            {data.role === "manager" ? "Manager" : "Worker"}
          </span>
        )}

        {isNode && data.state && (
          <span
            data-ready={data.state === "ready" || undefined}
            className="inline-block size-2 rounded-full bg-red-500 data-ready:bg-green-500"
          />
        )}

        {isNode && data.availability && data.availability !== "active" && (
          <span className="text-[10px] text-muted-foreground">{data.availability}</span>
        )}
      </div>
    </div>
  );
}
