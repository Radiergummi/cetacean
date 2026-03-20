import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Info } from "lucide-react";
import type React from "react";

type Row =
  | false
  | undefined
  | null
  | 0
  | ""
  | [string, React.ReactNode]
  | [string, React.ReactNode, string];

export default function KVTable({ rows }: { rows: Row[] }) {
  const valid = rows.filter(
    (row): row is [string, React.ReactNode] | [string, React.ReactNode, string] =>
      !!row && !!row[1],
  );

  if (valid.length === 0) {
    return null;
  }

  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full">
        <tbody>
          {valid.map(([key, value, tooltip]) => (
            <tr
              key={key}
              className="border-b last:border-b-0"
            >
              <td className="min-w-1/3 p-3 text-sm font-medium text-muted-foreground">
                <span className="inline-flex items-center gap-1.5">
                  {key}
                  {tooltip && (
                    <Tooltip>
                      <TooltipTrigger
                        render={<Info className="size-3.5 text-muted-foreground/50" />}
                      />
                      <TooltipContent>{tooltip}</TooltipContent>
                    </Tooltip>
                  )}
                </span>
              </td>
              <td className="p-3 font-mono text-xs break-all">{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
