import { LayoutGrid, TableProperties } from "lucide-react";
import type { ViewMode } from "../hooks/useViewMode";

interface Props {
  mode: ViewMode;
  onChange: (mode: ViewMode) => void;
}

export default function ViewToggle({ mode, onChange }: Props) {
  return (
    <div className="inline-flex rounded-md border shrink-0">
      <button
        onClick={() => onChange("table")}
        aria-pressed={mode === "table"}
        className="px-2.5 flex items-center text-muted-foreground hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground"
        title="Table view"
      >
        <TableProperties className="size-4" />
      </button>
      <button
        onClick={() => onChange("grid")}
        aria-pressed={mode === "grid"}
        className="px-2.5 flex items-center border-l text-muted-foreground hover:text-foreground aria-pressed:bg-muted aria-pressed:text-foreground"
        title="Grid view"
      >
        <LayoutGrid className="size-4" />
      </button>
    </div>
  );
}
