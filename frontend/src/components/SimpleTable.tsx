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
      data-max-height={maxHeight || undefined}
      className="overflow-x-auto rounded-lg border data-max-height:max-h-96 data-max-height:overflow-y-auto"
    >
      <table className="w-full min-w-max whitespace-nowrap">
        {columns && columns.length > 0 && (
          <thead className="sticky top-0 z-10 bg-background">
            <tr className="border-b bg-muted/50">
              {columns.map((column) => (
                <th
                  key={column}
                  className="p-3 text-left text-sm font-medium"
                >
                  {column}
                </th>
              ))}
            </tr>
          </thead>
        )}
        <tbody>
          {items.map((item, index) => (
            <tr
              key={keyFn(item, index)}
              className="border-b last:border-b-0"
            >
              {renderRow(item, index)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
