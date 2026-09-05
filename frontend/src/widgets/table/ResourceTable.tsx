import { columnsFor, type ResourceRecord } from "./columns";
import DataTable, { type Column } from "@/components/DataTable";
import { useMemo, useState } from "react";

interface Props {
  resourceType: string;
  records: ResourceRecord[];
  /** Count before paging, so the widget can say how much it is not showing. */
  total: number;
}

/**
 * The presentational half of the table widget: search, sort, and render.
 *
 * Split from TableWidget so it can be tested without a host bridge — the host
 * side is one `useToolData` call, and everything a reviewer would want to
 * exercise (does search filter? does the header sort?) lives here.
 *
 * Filtering and sorting are client-side over the page the server returned.
 * A widget renders in an iframe against one `find` call, so there is
 * no round trip to spend on a keystroke; `total` is what tells the reader when
 * they are looking at a subset.
 */
export function ResourceTable({ resourceType, records, total }: Props) {
  const [query, setQuery] = useState("");
  const [sortKey, setSortKey] = useState<string | undefined>(undefined);
  const [ascending, setAscending] = useState(true);

  const columns = useMemo(() => columnsFor(resourceType), [resourceType]);

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();

    const matched = needle
      ? records.filter((record) =>
          columns.some((column) => column.value(record).toLowerCase().includes(needle)),
        )
      : records;

    if (!sortKey) {
      return matched;
    }

    const column = columns.find(({ key }) => key === sortKey);

    if (!column) {
      return matched;
    }

    // Copied before sorting: the records array belongs to the caller's state.
    return [...matched].sort((left, right) => {
      const comparison = column.value(left).localeCompare(column.value(right));

      return ascending ? comparison : -comparison;
    });
  }, [records, columns, query, sortKey, ascending]);

  function toggleSort(key: string) {
    if (key === sortKey) {
      setAscending((previous) => !previous);

      return;
    }

    setSortKey(key);
    setAscending(true);
  }

  const tableColumns: Column<ResourceRecord>[] = columns.map((column) => ({
    header: column.header + (column.key === sortKey ? (ascending ? " ▲" : " ▼") : ""),
    cell: (record) => column.value(record) || "—",
    onHeaderClick: () => toggleSort(column.key),
  }));

  return (
    <div className="flex flex-col gap-2 p-2">
      <div className="flex items-center justify-between gap-3">
        <input
          type="search"
          value={query}
          placeholder={`Filter ${resourceType}…`}
          aria-label={`Filter ${resourceType}`}
          className="w-full max-w-xs rounded border px-2 py-1 text-sm"
          onChange={({ target }) => setQuery(target.value)}
        />

        <p className="shrink-0 text-xs text-muted-foreground">
          {visible.length === total
            ? `${total} ${resourceType}`
            : `${visible.length} of ${total} ${resourceType}`}
        </p>
      </div>

      {visible.length === 0 ? (
        <p className="p-3 text-sm opacity-70">No {resourceType} match this filter.</p>
      ) : (
        <DataTable
          columns={tableColumns}
          data={visible}
          keyFn={({ id }) => id}
        />
      )}
    </div>
  );
}
