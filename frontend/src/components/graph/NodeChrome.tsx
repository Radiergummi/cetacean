import { Handle, Position } from "@xyflow/react";
import type { ReactNode } from "react";

export const focusRing =
  "focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none";

/**
 * Every node carries both handles so an edge always finds an anchor; the one
 * a given column does not use is invisible rather than absent.
 */
export function Ports() {
  return (
    <>
      <Handle
        type="target"
        position={Position.Left}
        className="opacity-0"
        isConnectable={false}
      />
      <Handle
        type="source"
        position={Position.Right}
        className="opacity-0"
        isConnectable={false}
      />
    </>
  );
}

export function DetailList({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5">{children}</dl>;
}

export function Detail({ term, children }: { term: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{term}</dt>
      <dd className="font-mono break-all">{children}</dd>
    </>
  );
}
