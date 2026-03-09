import { ChevronUp, ChevronDown, ChevronsUpDown } from "lucide-react";
import type { SortDir } from "../hooks/useSort";

export default function SortIndicator({
  label,
  active,
  dir,
}: {
  label: string;
  active: boolean;
  dir: SortDir;
}) {
  return (
    <span className="inline-flex items-center gap-1">
      {label}
      {active ? (
        dir === "asc" ? (
          <ChevronUp className="w-3.5 h-3.5" />
        ) : (
          <ChevronDown className="w-3.5 h-3.5" />
        )
      ) : (
        <ChevronsUpDown className="w-3.5 h-3.5 opacity-30" />
      )}
    </span>
  );
}
