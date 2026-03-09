import type { NodeProps } from "@xyflow/react";

type GroupData = {
  label: string;
  role?: string;
  state?: string;
  availability?: string;
  variant: "stack" | "node";
};

export default function GroupNode({ data }: NodeProps & { data: GroupData }) {
  const isNode = data.variant === "node";

  return (
    <div className="border border-dashed rounded-xl bg-muted/10 p-4 min-w-[200px] min-h-[100px]">
      <div className="flex items-center gap-2 mb-2">
        <span className="text-sm font-semibold text-muted-foreground">{data.label}</span>

        {isNode && data.role && (
          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-muted text-muted-foreground">
            {data.role === "manager" ? "Manager" : "Worker"}
          </span>
        )}

        {isNode && data.state && (
          <span
            className={`inline-block w-2 h-2 rounded-full ${data.state === "ready" ? "bg-green-500" : "bg-red-500"}`}
          />
        )}

        {isNode && data.availability && data.availability !== "active" && (
          <span className="text-[10px] text-muted-foreground">{data.availability}</span>
        )}
      </div>
    </div>
  );
}
