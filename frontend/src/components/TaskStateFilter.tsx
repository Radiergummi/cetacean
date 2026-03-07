import { useMemo } from "react";
import type { Task } from "../api/types";

export default function TaskStateFilter({
  tasks,
  active,
  onChange,
}: {
  tasks: Task[];
  active: string | null;
  onChange: (state: string | null) => void;
}) {
  const counts = useMemo(() => {
    const m = new Map<string, number>();
    for (const t of tasks) {
      m.set(t.Status.State, (m.get(t.Status.State) || 0) + 1);
    }
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [tasks]);

  const orbClass = (isActive: boolean) =>
    `inline-flex items-center justify-center min-w-5 h-5 px-1.5 rounded-full text-[10px] font-semibold tabular-nums ${
      isActive ? "bg-background/30" : "bg-foreground/10"
    }`;

  return (
    <div className="flex items-center gap-1.5 flex-wrap">
      <button
        onClick={() => onChange(null)}
        className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium transition-colors ${
          active === null
            ? "bg-foreground text-background"
            : "bg-muted text-muted-foreground hover:text-foreground"
        }`}
      >
        All
        <span className={orbClass(active === null)}>{tasks.length}</span>
      </button>
      {counts.map(([state, count]) => (
        <button
          key={state}
          onClick={() => onChange(active === state ? null : state)}
          className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium transition-colors ${
            active === state
              ? "bg-foreground text-background"
              : "bg-muted text-muted-foreground hover:text-foreground"
          }`}
        >
          {state}
          <span className={orbClass(active === state)}>{count}</span>
        </button>
      ))}
    </div>
  );
}
