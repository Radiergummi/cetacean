import type React from "react";

export default function KeyValuePills({
  entries,
  renderValue,
  onCopy,
}: {
  entries: [string, string][];
  renderValue?: (value: string) => React.ReactNode;
  onCopy?: React.ClipboardEventHandler;
}) {
  return (
    <ul
      className="flex flex-wrap gap-2"
      onCopy={onCopy}
    >
      {entries.map(([key, value]) => (
        <li
          key={key}
          className="inline-flex items-baseline overflow-hidden rounded-md border font-mono text-xs"
        >
          <span className="bg-muted/50 px-2 py-1 whitespace-nowrap text-muted-foreground">
            {key}
          </span>
          {value ? (
            <span className="truncate px-2 py-1">{renderValue ? renderValue(value) : value}</span>
          ) : undefined}
        </li>
      ))}
    </ul>
  );
}
