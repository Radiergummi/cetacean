import type { ViewMode } from "../hooks/useViewMode";
import { SearchInput } from "./search";
import ViewToggle from "./ViewToggle";

interface Props {
  search: string;
  onSearchChange: (value: string) => void;
  placeholder?: string;
  viewMode?: ViewMode;
  onViewModeChange?: (mode: ViewMode) => void;
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
        <ViewToggle
          mode={viewMode}
          onChange={onViewModeChange}
        />
      )}
    </div>
  );
}
