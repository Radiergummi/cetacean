import type { SortDir } from "../hooks/useSort";
import { ChevronDown, ChevronsUpDown, ChevronUp } from "lucide-react";

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
          <ChevronUp className="size-3.5" />
        ) : (
          <ChevronDown className="size-3.5" />
        )
      ) : (
        <ChevronsUpDown className="size-3.5 opacity-30" />
      )}
    </span>
  );
}
