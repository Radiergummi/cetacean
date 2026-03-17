import type React from "react";

export default function SimpleTable<T>({
  columns,
  items,
  keyFn,
  renderRow,
  maxHeight,
}: {
  columns?: string[];
  items: T[];
  keyFn: (item: T, index: number) => string | number;
  renderRow: (item: T, index: number) => React.ReactNode;
  maxHeight?: boolean;
}) {
  return (
    <div
      className={`overflow-x-auto rounded-lg border ${maxHeight ? "max-h-96 overflow-y-auto" : ""}`}
    >
      <table className="w-full min-w-max whitespace-nowrap">
        {columns && columns.length > 0 && (
          <thead className="sticky top-0 z-10 bg-background">
            <tr className="border-b bg-muted/50">
              {columns.map((col) => (
                <th
                  key={col}
                  className="p-3 text-left text-sm font-medium"
                >
                  {col}
                </th>
              ))}
            </tr>
          </thead>
        )}
        <tbody>
          {items.map((item, i) => (
            <tr
              key={keyFn(item, i)}
              className="border-b last:border-b-0"
            >
              {renderRow(item, i)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
