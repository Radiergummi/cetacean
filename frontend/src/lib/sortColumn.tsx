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
): { header: ReactNode; onHeaderClick: () => void } {
  return {
    header: (
      <SortIndicator
        label={label}
        active={sortKey === key}
        dir={sortDir}
      />
    ),
    onHeaderClick: () => toggle(key),
  };
}
