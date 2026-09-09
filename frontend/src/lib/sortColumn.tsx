import SortIndicator from "../components/SortIndicator";
import type { SortDir } from "../hooks/useSort";
import type { ReactNode } from "react";

/**
 * Returns `header` and `onHeaderClick` props for a sortable DataTable column.
 */
export function sortColumn(
  label: string,
  key: string,
  sortKey: string | undefined,
  sortDir: SortDir,
  toggle: (key: string) => void,
): {
  header: ReactNode;
  onHeaderClick: () => void;
  sortDirection: "ascending" | "descending" | "none";
} {
  const active = sortKey === key;

  return {
    header: (
      <SortIndicator
        label={label}
        active={active}
        dir={sortDir}
      />
    ),
    onHeaderClick: () => toggle(key),
    // `aria-sort` on the header is what announces the sort. Without it the
    // only cue was the chevron, which a screen reader never sees.
    sortDirection: !active ? "none" : sortDir === "asc" ? "ascending" : "descending",
  };
}
