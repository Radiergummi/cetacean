import { useLatestRef } from "@/hooks/useLatestRef";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";

interface Column<T> {
  header: ReactNode;
  cell: (item: T) => ReactNode;
  className?: string | undefined;
  onHeaderClick?: (() => void) | undefined;
  /** Announced on the column header; set by `sortColumn` for sortable ones. */
  sortDirection?: "ascending" | "descending" | "none" | undefined;
}

interface Props<T> {
  columns: Column<T>[];
  data: T[];
  keyFn: (item: T) => string;
  rowClassName?: ((item: T) => string) | undefined;
  onRowClick?: ((item: T) => void) | undefined;
  hasMore?: boolean | undefined;
  onLoadMore?: (() => void) | undefined;
  /** Names the grid for assistive technology. */
  label?: string | undefined;
}

const virtualThreshold = 100;
const rowHeightEstimate = 48;

function PlainBody<T>({
  columns,
  data,
  keyFn,
  rowClassName,
  onRowClick,
  selectedIndex,
  rowId,
}: Props<T> & { selectedIndex: number; rowId: (index: number) => string }) {
  return (
    <tbody>
      {data.map((item, index) => (
        <tr
          key={keyFn(item)}
          id={rowId(index)}
          data-clickable={onRowClick ? "" : undefined}
          data-selected={index === selectedIndex || undefined}
          aria-selected={onRowClick ? index === selectedIndex : undefined}
          className={`border-b last:border-b-0 data-clickable:cursor-pointer data-clickable:hover:bg-muted/50 data-selected:bg-accent data-selected:text-accent-foreground ${
            rowClassName?.(item) ?? ""
          }`}
          onClick={onRowClick ? () => onRowClick(item) : undefined}
        >
          {columns.map((column, colIndex) => (
            <td
              key={colIndex}
              className={`p-3 text-sm whitespace-nowrap ${column.className ?? ""}`}
            >
              {column.cell(item)}
            </td>
          ))}
        </tr>
      ))}
    </tbody>
  );
}

function VirtualBody<T>({
  columns,
  data,
  keyFn,
  rowClassName,
  onRowClick,
  scrollRef,
  selectedIndex,
  rowId,
}: Props<T> & {
  scrollRef: RefObject<HTMLDivElement | null>;
  selectedIndex: number;
  rowId: (index: number) => string;
}) {
  // oxlint-disable-next-line react/incompatible-library -- `useVirtualizer` returns functions React Compiler cannot memoize, so it skips memoizing this component. That is the library's shape, not a fixable call site.
  const virtualizer = useVirtualizer({
    count: data.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeightEstimate,
    overscan: 20,
  });

  const virtualItems = virtualizer.getVirtualItems();
  const totalSize = virtualizer.getTotalSize();

  // Scroll selected row into view
  useEffect(() => {
    if (selectedIndex >= 0) {
      virtualizer.scrollToIndex(selectedIndex, { align: "auto" });
    }
  }, [selectedIndex, virtualizer]);

  const firstVirtualItem = virtualItems[0];
  const lastVirtualItem = virtualItems[virtualItems.length - 1];

  return (
    <tbody>
      {firstVirtualItem && (
        <tr
          role="presentation"
          data-virtual-row=""
        >
          <td
            role="presentation"
            style={{ height: firstVirtualItem.start, padding: 0 }}
            colSpan={columns.length}
          />
        </tr>
      )}
      {virtualItems.map(({ index }) => {
        const item = data[index];

        if (!item) {
          return null;
        }

        return (
          <tr
            key={keyFn(item)}
            ref={virtualizer.measureElement}
            id={rowId(index)}
            data-index={index}
            data-virtual-row=""
            data-stripe={index % 2 === 1 || undefined}
            data-clickable={onRowClick ? "" : undefined}
            data-selected={index === selectedIndex || undefined}
            aria-selected={onRowClick ? index === selectedIndex : undefined}
            data-last={index === data.length - 1 || undefined}
            className={`border-b data-clickable:cursor-pointer data-clickable:hover:bg-muted/50 data-last:border-b-0 data-selected:bg-accent data-selected:text-accent-foreground ${
              rowClassName?.(item) ?? ""
            }`}
            onClick={onRowClick ? () => onRowClick(item) : undefined}
          >
            {columns.map((column, columnIndex) => (
              <td
                key={columnIndex}
                className={`p-3 text-sm whitespace-nowrap ${column.className ?? ""}`}
              >
                {column.cell(item)}
              </td>
            ))}
          </tr>
        );
      })}
      {lastVirtualItem && (
        <tr
          role="presentation"
          data-virtual-row=""
        >
          <td
            role="presentation"
            style={{
              height: Math.max(0, totalSize - lastVirtualItem.end),
              padding: 0,
            }}
            colSpan={columns.length}
          />
        </tr>
      )}
    </tbody>
  );
}

export default function DataTable<T>({
  columns,
  data,
  keyFn,
  rowClassName,
  onRowClick,
  hasMore,
  onLoadMore,
  label,
}: Props<T>) {
  const gridId = useId();
  const rowId = useCallback((index: number) => `${gridId}-row-${index}`, [gridId]);
  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLTableRowElement>(null);
  const useVirtual = data.length > virtualThreshold;
  const [selectedIndex, setSelectedIndex] = useState(-1);

  const prevVirtualRef = useRef(useVirtual);

  // Keep the cursor pointing at a row that still exists, rather than dropping
  // it whenever `data` changes. The list hooks rebuild that array on every
  // render and an SSE event rewrites the rows it holds, so keying a reset on
  // its identity cleared the selection under the person's hands on any live
  // collection. Only a list that shrank past the cursor has to move it, and
  // that is settled during render so no pass ever commits an
  // `aria-activedescendant` naming a row that is no longer there.
  if (selectedIndex >= data.length) {
    setSelectedIndex(data.length - 1);
  }

  // Reset scroll when switching render mode
  useEffect(() => {
    if (prevVirtualRef.current !== useVirtual && scrollRef.current) {
      scrollRef.current.scrollTop = 0;
    }

    prevVirtualRef.current = useVirtual;
  }, [useVirtual]);

  // Scroll selected plain row into view
  useEffect(() => {
    if (useVirtual || selectedIndex < 0) {
      return;
    }

    const row = scrollRef.current?.querySelector(`tbody tr:nth-child(${selectedIndex + 1})`);
    row?.scrollIntoView({ block: "nearest" });
  }, [selectedIndex, useVirtual]);

  const onLoadMoreRef = useLatestRef(onLoadMore);

  useEffect(() => {
    if (!hasMore || !onLoadMoreRef.current || !sentinelRef.current) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            onLoadMoreRef.current?.();
          }
        }
      },
      { root: scrollRef.current, rootMargin: "200px" },
    );

    observer.observe(sentinelRef.current);

    return () => observer.disconnect();
  }, [hasMore, onLoadMoreRef]);

  const onKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (!data.length) {
        return;
      }

      switch (event.key) {
        case "ArrowDown":
        case "j":
          event.preventDefault();
          setSelectedIndex((index) => Math.min(index + 1, data.length - 1));
          break;

        case "ArrowUp":
        case "k":
          event.preventDefault();
          setSelectedIndex((index) => Math.max(index - 1, 0));
          break;

        case "Enter":
          {
            const selected = data[selectedIndex];

            if (selected && onRowClick) {
              event.preventDefault();
              onRowClick(selected);
            }
          }

          break;
      }
    },
    [data, selectedIndex, onRowClick],
  );

  return (
    <div
      ref={scrollRef}
      data-virtual={useVirtual || undefined}
      className="overflow-x-auto rounded-lg border data-virtual:max-h-[calc(100vh-16rem)] data-virtual:overflow-y-auto"
    >
      {/*
        The grid, not the scroll container, is what takes focus: the cursor it
        moves is published through `aria-activedescendant`, which has to sit on
        the element owning the rows it points at.
      */}
      <table
        role="grid"
        aria-label={label}
        aria-activedescendant={selectedIndex >= 0 ? rowId(selectedIndex) : undefined}
        tabIndex={0}
        onKeyDown={onKeyDown}
        className="w-full min-w-max outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <thead className="sticky top-0 z-10 bg-background">
          <tr className="border-b bg-muted/50">
            {columns.map((column, index) => (
              <th
                key={index}
                aria-sort={column.sortDirection}
                className={`text-left text-sm font-medium ${column.className ?? ""}`}
              >
                {/*
                  A sortable header is a real button. It was a click handler on
                  the `<th>`, which left sorting reachable by mouse only and
                  announced the column as plain text.
                */}
                {column.onHeaderClick ? (
                  <button
                    type="button"
                    onClick={column.onHeaderClick}
                    className="flex w-full cursor-pointer items-center p-3 text-left font-medium select-none hover:bg-muted/80 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
                  >
                    {column.header}
                  </button>
                ) : (
                  <span className="block p-3">{column.header}</span>
                )}
              </th>
            ))}
          </tr>
        </thead>

        {useVirtual ? (
          <VirtualBody
            columns={columns}
            data={data}
            keyFn={keyFn}
            rowClassName={rowClassName}
            onRowClick={onRowClick}
            scrollRef={scrollRef}
            selectedIndex={selectedIndex}
            rowId={rowId}
          />
        ) : (
          <PlainBody
            columns={columns}
            data={data}
            keyFn={keyFn}
            rowClassName={rowClassName}
            onRowClick={onRowClick}
            selectedIndex={selectedIndex}
            rowId={rowId}
          />
        )}

        {hasMore && (
          <tfoot>
            <tr
              ref={sentinelRef}
              data-testid="load-more-sentinel"
            >
              <td
                colSpan={columns.length}
                className="p-3 text-center text-sm text-muted-foreground"
              >
                Loading…
              </td>
            </tr>
          </tfoot>
        )}
      </table>
    </div>
  );
}

export type { Column };
