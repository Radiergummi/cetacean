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
        className={`px-2.5 flex items-center ${mode === "table" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
        title="Table view"
      >
        <TableProperties className="w-4 h-4" />
      </button>
      <button
        onClick={() => onChange("grid")}
        className={`px-2.5 flex items-center border-l ${mode === "grid" ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground"}`}
        title="Grid view"
      >
        <LayoutGrid className="w-4 h-4" />
      </button>
    </div>
  );
}
