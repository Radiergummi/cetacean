import type { ViewMode } from "../hooks/useViewMode";
import { SearchInput } from "./search";
import ViewToggle, { type View } from "./ViewToggle";
import { LayoutGrid, TableProperties } from "lucide-react";

const listViews: View<ViewMode>[] = [
  { value: "table", label: "Table view", icon: <TableProperties className="size-4" /> },
  { value: "grid", label: "Grid view", icon: <LayoutGrid className="size-4" /> },
];

interface Props {
  search: string;
  onSearchChange: (value: string) => void;
  placeholder?: string | undefined;
  viewMode?: ViewMode | undefined;
  onViewModeChange?: ((mode: ViewMode) => void) | undefined;
}

export default function ListToolbar({
  search,
  onSearchChange,
  placeholder,
  viewMode,
  onViewModeChange,
}: Props) {
  return (
    <div className="mb-4 flex items-stretch gap-3">
      <SearchInput
        value={search}
        onChange={onSearchChange}
        placeholder={placeholder}
      />
      {viewMode != null && onViewModeChange && (
        <div className="hidden md:flex">
          <ViewToggle
            views={listViews}
            value={viewMode}
            onChange={onViewModeChange}
            className="h-auto"
          />
        </div>
      )}
    </div>
  );
}
