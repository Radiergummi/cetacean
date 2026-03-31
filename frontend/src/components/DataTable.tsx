import { useVirtualizer } from "@tanstack/react-virtual";
import { type ReactNode, type RefObject, useCallback, useEffect, useRef, useState } from "react";

interface Column<T> {
  header: ReactNode;
  cell: (item: T) => ReactNode;
  className?: string;
  onHeaderClick?: () => void;
}

interface Props<T> {
  columns: Column<T>[];
  data: T[];
  keyFn: (item: T) => string;
  rowClassName?: (item: T) => string;
  onRowClick?: (item: T) => void;
}

const VIRTUAL_THRESHOLD = 100;
const ROW_HEIGHT_ESTIMATE = 48;

function PlainBody<T>({
  columns,
  data,
  keyFn,
  rowClassName,
  onRowClick,
  selectedIndex,
}: Props<T> & { selectedIndex: number }) {
  return (
    <tbody>
      {data.map((item, index) => (
        <tr
          key={keyFn(item)}
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
}: Props<T> & { scrollRef: RefObject<HTMLDivElement | null>; selectedIndex: number }) {
  const virtualizer = useVirtualizer({
    count: data.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT_ESTIMATE,
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

  return (
    <tbody>
      {virtualItems.length > 0 && (
        <tr>
          <td
            style={{ height: virtualItems[0].start, padding: 0 }}
            colSpan={columns.length}
          />
        </tr>
      )}
      {virtualItems.map(({ index }) => {
        const item = data[index];

        return (
          <tr
            key={keyFn(item)}
            ref={virtualizer.measureElement}
            data-index={index}
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
      {virtualItems.length > 0 && (
        <tr>
          <td
            style={{
              height: Math.max(0, totalSize - virtualItems[virtualItems.length - 1].end),
              padding: 0,
            }}
            colSpan={columns.length}
          />
        </tr>
      )}
    </tbody>
  );
}

export default function DataTable<T>({ columns, data, keyFn, rowClassName, onRowClick }: Props<T>) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const useVirtual = data.length > VIRTUAL_THRESHOLD;
  const [selectedIndex, setSelectedIndex] = useState(-1);

  const prevVirtualRef = useRef(useVirtual);

  // Reset selection when data changes; reset scroll when switching render mode
  useEffect(() => {
    setSelectedIndex(-1);

    if (prevVirtualRef.current !== useVirtual && scrollRef.current) {
      scrollRef.current.scrollTop = 0;
    }

    prevVirtualRef.current = useVirtual;
  }, [data, useVirtual]);

  // Scroll selected plain row into view
  useEffect(() => {
    if (useVirtual || selectedIndex < 0) {
      return;
    }

    const row = scrollRef.current?.querySelector(`tbody tr:nth-child(${selectedIndex + 1})`);
    row?.scrollIntoView({ block: "nearest" });
  }, [selectedIndex, useVirtual]);

  const onKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
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
          if (selectedIndex >= 0 && selectedIndex < data.length && onRowClick) {
            event.preventDefault();
            onRowClick(data[selectedIndex]);
          }

          break;
      }
    },
    [data, selectedIndex, onRowClick],
  );

  return (
    <div
      ref={scrollRef}
      tabIndex={0}
      data-virtual={useVirtual || undefined}
      className="overflow-x-auto rounded-lg border outline-none focus-visible:ring-3 focus-visible:ring-ring/50 data-virtual:max-h-[calc(100vh-16rem)] data-virtual:overflow-y-auto"
      onKeyDown={onKeyDown}
    >
      <table className="w-full min-w-max">
        <thead className="sticky top-0 z-10 bg-background">
          <tr className="border-b bg-muted/50">
            {columns.map((column, index) => (
              <th
                key={index}
                data-clickable={column.onHeaderClick ? "" : undefined}
                className={`p-3 text-left text-sm font-medium ${
                  column.className ?? ""
                } data-clickable:cursor-pointer data-clickable:select-none data-clickable:hover:bg-muted/80`}
                onClick={column.onHeaderClick}
              >
                {column.header}
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
          />
        ) : (
          <PlainBody
            columns={columns}
            data={data}
            keyFn={keyFn}
            rowClassName={rowClassName}
            onRowClick={onRowClick}
            selectedIndex={selectedIndex}
          />
        )}
      </table>
    </div>
  );
}

export type { Column };
