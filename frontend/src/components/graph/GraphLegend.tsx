import type { ReactNode } from "react";

export type LegendEntry = { mark: ReactNode; label: string };

/** A node dashed or muted says so only in its tooltip, which costs a hover. */
export function GraphLegend({ entries }: { entries: LegendEntry[] }) {
  return (
    <ul
      data-testid="graph-legend"
      className="pointer-events-none flex flex-wrap gap-x-3 gap-y-1 rounded-md border bg-card/80 px-2 py-1 text-xs text-muted-foreground"
    >
      {entries.map(({ mark, label }) => (
        <li
          key={label}
          className="flex items-center gap-1"
        >
          {mark}
          {label}
        </li>
      ))}
    </ul>
  );
}

export const dashedMark = <span className="size-3 rounded-sm border border-dashed" />;
export const mutedMark = <span className="size-3 rounded-sm border bg-muted" />;
