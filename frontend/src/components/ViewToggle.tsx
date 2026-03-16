import type { ViewMode } from "../hooks/useViewMode";
import { LayoutGrid, TableProperties } from "lucide-react";

interface Props {
  mode: ViewMode;
  onChange: (mode: ViewMode) => void;
}

export default function ViewToggle({ mode, onChange }: Props) {
  return (
    <div className="inline-flex shrink-0 rounded-md border">
      <button
        onClick={() => onChange("table")}
        aria-pressed={mode === "table"}
        className="flex items-center px-2.5 text-muted-foreground hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground"
        title="Table view"
      >
        <TableProperties className="size-4" />
      </button>
      <button
        onClick={() => onChange("grid")}
        aria-pressed={mode === "grid"}
        className="flex items-center border-l px-2.5 text-muted-foreground hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground"
        title="Grid view"
      >
        <LayoutGrid className="size-4" />
      </button>
    </div>
  );
}
