import { ChevronUp, ChevronDown, ChevronsUpDown } from "lucide-react";
import type { SortDir } from "../hooks/useSort";

interface Props {
  label: string;
  sortKey: string;
  activeSortKey?: string;
  sortDir: SortDir;
  onToggle: (key: string) => void;
}

export default function SortableHeader({
  label,
  sortKey,
  activeSortKey,
  sortDir,
  onToggle,
}: Props) {
  const active = sortKey === activeSortKey;
  return (
    <th
      className="text-left p-3 text-sm font-medium cursor-pointer select-none hover:bg-muted/80"
      onClick={() => onToggle(sortKey)}
    >
      <span className="inline-flex items-center gap-1">
        {label}
        {active ? (
          sortDir === "asc" ? (
            <ChevronUp className="w-3.5 h-3.5" />
          ) : (
            <ChevronDown className="w-3.5 h-3.5" />
          )
        ) : (
          <ChevronsUpDown className="w-3.5 h-3.5 opacity-30" />
        )}
      </span>
    </th>
  );
}
