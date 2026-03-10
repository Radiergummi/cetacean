import { useState, useCallback } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { LogLine } from "./log-utils";
import { LEVEL_BAR, formatTime, isJSON, LOG_ROW_HEIGHT_ESTIMATE, LOG_VIRTUAL_THRESHOLD } from "./log-utils";
import { LogMessage } from "./LogMessage";

export interface LogTableProps {
  containerRef: React.RefObject<HTMLDivElement | null>;
  handleScroll: () => void;
  filtered: LogLine[];
  showAttrs: boolean;
  wrapLines: boolean;
  search: string;
  caseSensitive: boolean;
}

function LogRow({
  line,
  showAttrs,
  wrapLines,
  search,
  caseSensitive,
  isExpanded,
  onToggle,
  measureRef,
  dataIndex,
}: {
  line: LogLine;
  showAttrs: boolean;
  wrapLines: boolean;
  search: string;
  caseSensitive: boolean;
  isExpanded: boolean;
  onToggle: ((index: number) => void) | undefined;
  measureRef?: (el: HTMLElement | null) => void;
  dataIndex?: number;
}) {
  const jsonLine = isJSON(line.message);
  const prettyPrint = jsonLine && (wrapLines || isExpanded);
  return (
    <tr
      key={line.index}
      ref={measureRef}
      data-index={dataIndex}
      className={`hover:bg-muted/50 group ${jsonLine ? "cursor-pointer" : ""}`}
      onClick={onToggle && jsonLine ? () => onToggle(line.index) : undefined}
    >
      <td className="w-[3px] p-0 align-stretch">
        <div className={`w-[3px] min-h-full ${LEVEL_BAR[line.level]}`} />
      </td>
      <td className="pl-2 pr-1 py-px text-muted-foreground/50 text-right select-none align-top tabular-nums">
        {line.index + 1}
      </td>
      <td
        className="px-2 py-px text-muted-foreground whitespace-nowrap align-top select-all"
        title={line.timestamp}
      >
        {formatTime(line.timestamp)}
      </td>
      {showAttrs && (
        <td
          className="px-2 py-px text-muted-foreground/60 whitespace-nowrap align-top font-mono"
          title={line.attrs?.taskId}
        >
          {line.attrs?.taskId?.slice(0, 8)}
        </td>
      )}
      <td
        className={`px-2 py-px text-foreground ${wrapLines || isExpanded ? "whitespace-pre-wrap break-all" : "whitespace-pre"}`}
      >
        <LogMessage line={line} search={search} caseSensitive={caseSensitive} prettyJson={prettyPrint} />
      </td>
    </tr>
  );
}

export function LogTable({ containerRef, handleScroll, filtered, showAttrs, wrapLines, search, caseSensitive }: LogTableProps) {
  const [expanded, setExpanded] = useState<Set<number>>(() => new Set());
  const useVirtual = filtered.length > LOG_VIRTUAL_THRESHOLD;

  const toggleExpanded = useCallback((index: number) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  }, []);

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="log-panel overflow-auto"
    >
      <table className="w-full border-collapse font-mono text-xs leading-5">
        {useVirtual ? (
          <VirtualLogBody
            containerRef={containerRef}
            filtered={filtered}
            showAttrs={showAttrs}
            wrapLines={wrapLines}
            search={search}
            caseSensitive={caseSensitive}
            expanded={expanded}
            toggleExpanded={toggleExpanded}
          />
        ) : (
          <tbody>
            {filtered.map((line) => (
              <LogRow
                key={line.index}
                line={line}
                showAttrs={showAttrs}
                wrapLines={wrapLines}
                search={search}
                caseSensitive={caseSensitive}
                isExpanded={expanded.has(line.index)}
                onToggle={toggleExpanded}
              />
            ))}
          </tbody>
        )}
      </table>
    </div>
  );
}

function VirtualLogBody({
  containerRef,
  filtered,
  showAttrs,
  wrapLines,
  search,
  caseSensitive,
  expanded,
  toggleExpanded,
}: {
  containerRef: React.RefObject<HTMLDivElement | null>;
  filtered: LogLine[];
  showAttrs: boolean;
  wrapLines: boolean;
  search: string;
  caseSensitive: boolean;
  expanded: Set<number>;
  toggleExpanded: (index: number) => void;
}) {
  const virtualizer = useVirtualizer({
    count: filtered.length,
    getScrollElement: () => containerRef.current,
    estimateSize: () => LOG_ROW_HEIGHT_ESTIMATE,
    overscan: 50,
  });

  const virtualItems = virtualizer.getVirtualItems();
  const totalSize = virtualizer.getTotalSize();
  const colCount = showAttrs ? 5 : 4;

  return (
    <tbody>
      {virtualItems.length > 0 && (
        <tr>
          <td style={{ height: virtualItems[0].start, padding: 0 }} colSpan={colCount} />
        </tr>
      )}
      {virtualItems.map((virtualRow) => {
        const line = filtered[virtualRow.index];
        return (
          <LogRow
            key={line.index}
            line={line}
            showAttrs={showAttrs}
            wrapLines={wrapLines}
            search={search}
            caseSensitive={caseSensitive}
            isExpanded={expanded.has(line.index)}
            onToggle={toggleExpanded}
            measureRef={virtualizer.measureElement}
            dataIndex={virtualRow.index}
          />
        );
      })}
      {virtualItems.length > 0 && (
        <tr>
          <td
            style={{ height: Math.max(0, totalSize - virtualItems[virtualItems.length - 1].end), padding: 0 }}
            colSpan={colCount}
          />
        </tr>
      )}
    </tbody>
  );
}
