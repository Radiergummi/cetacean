import type React from "react";

type Row = false | undefined | null | 0 | "" | [string, React.ReactNode];

export default function KVTable({ rows }: { rows: Row[] }) {
  const valid = rows.filter((row): row is [string, React.ReactNode] => !!row && !!row[1]);
  if (valid.length === 0) return null;

  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full">
        <tbody>
          {valid.map(([key, value]) => (
            <tr key={key} className="border-b last:border-b-0">
              <td className="p-3 text-sm font-medium text-muted-foreground min-w-1/3">{key}</td>
              <td className="p-3 font-mono text-xs break-all">{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
