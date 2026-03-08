import { type ReactNode, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";

interface Column<T> {
  header: ReactNode;
  cell: (item: T) => ReactNode;
  className?: string;
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

function PlainBody<T>({ columns, data, keyFn, rowClassName, onRowClick }: Props<T>) {
  return (
    <tbody>
      {data.map((item) => (
        <tr
          key={keyFn(item)}
          className={`border-b ${onRowClick ? "cursor-pointer hover:bg-muted/50" : ""} ${rowClassName?.(item) ?? ""}`}
          onClick={onRowClick ? () => onRowClick(item) : undefined}
        >
          {columns.map((col, i) => (
            <td key={i} className={`p-3 text-sm ${col.className ?? ""}`}>
              {col.cell(item)}
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
}: Props<T> & { scrollRef: React.RefObject<HTMLDivElement | null> }) {
  const virtualizer = useVirtualizer({
    count: data.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT_ESTIMATE,
    overscan: 20,
  });

  const virtualItems = virtualizer.getVirtualItems();
  const totalSize = virtualizer.getTotalSize();

  return (
    <tbody>
      {virtualItems.length > 0 && (
        <tr>
          <td style={{ height: virtualItems[0].start, padding: 0 }} colSpan={columns.length} />
        </tr>
      )}
      {virtualItems.map((virtualRow) => {
        const item = data[virtualRow.index];
        return (
          <tr
            key={keyFn(item)}
            ref={virtualizer.measureElement}
            data-index={virtualRow.index}
            className={`border-b ${onRowClick ? "cursor-pointer hover:bg-muted/50" : ""} ${rowClassName?.(item) ?? ""}`}
            onClick={onRowClick ? () => onRowClick(item) : undefined}
          >
            {columns.map((col, i) => (
              <td key={i} className={`p-3 text-sm ${col.className ?? ""}`}>
                {col.cell(item)}
              </td>
            ))}
          </tr>
        );
      })}
      {virtualItems.length > 0 && (
        <tr>
          <td
            style={{ height: totalSize - virtualItems[virtualItems.length - 1].end, padding: 0 }}
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

  return (
    <div
      ref={scrollRef}
      className={`overflow-x-auto rounded-lg border ${useVirtual ? "max-h-[calc(100vh-16rem)] overflow-y-auto" : ""}`}
    >
      <table className="w-full">
        <thead className="sticky top-0 z-10 bg-background">
          <tr className="border-b bg-muted/50">
            {columns.map((col, i) => (
              <th key={i} className={`text-left p-3 text-sm font-medium ${col.className ?? ""}`}>
                {col.header}
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
          />
        ) : (
          <PlainBody
            columns={columns}
            data={data}
            keyFn={keyFn}
            rowClassName={rowClassName}
            onRowClick={onRowClick}
          />
        )}
      </table>
    </div>
  );
}

export type { Column };
