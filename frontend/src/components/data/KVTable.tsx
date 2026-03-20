import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Check, Copy, Info } from "lucide-react";
import type React from "react";
import { useState } from "react";

type Row =
  | false
  | undefined
  | null
  | 0
  | ""
  | [string, React.ReactNode]
  | [string, React.ReactNode, string]
  | [string, React.ReactNode, string | undefined, string];

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    navigator.clipboard.writeText(text).then(
      () => {
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      },
      () => {},
    );
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="ml-auto shrink-0 cursor-pointer rounded p-1 text-muted-foreground/50 hover:text-muted-foreground"
    >
      {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
    </button>
  );
}

export default function KVTable({ rows }: { rows: Row[] }) {
  const valid = rows.filter(
    (row): row is
      | [string, React.ReactNode]
      | [string, React.ReactNode, string]
      | [string, React.ReactNode, string | undefined, string] =>
      !!row && !!row[1],
  );

  if (valid.length === 0) {
    return null;
  }

  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full">
        <tbody>
          {valid.map(([key, value, tooltip, copyText]) => {
            const copyable = copyText ?? (typeof value === "string" ? value : undefined);

            return (
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
                <td className="p-3 font-mono text-xs break-all">
                  <span className="flex items-center gap-2">
                    <span className="min-w-0">{value}</span>
                    {copyable && <CopyButton text={copyable} />}
                  </span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
