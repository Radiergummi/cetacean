import type { ReactNode } from "react";

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

export default function DataTable<T>({ columns, data, keyFn, rowClassName, onRowClick }: Props<T>) {
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full">
        <thead className="sticky top-0 z-10 bg-background">
          <tr className="border-b bg-muted/50">
            {columns.map((col, i) => (
              <th
                key={i}
                className={`text-left p-3 text-sm font-medium ${col.className ?? ""}`}
              >
                {col.header}
              </th>
            ))}
          </tr>
        </thead>
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
      </table>
    </div>
  );
}

export type { Column };
