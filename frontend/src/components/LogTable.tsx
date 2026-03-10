import { useState, useCallback, useEffect } from "react";
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
  highlightIndex?: number;
  scrollToFiltered?: number;
  loadingOlder?: boolean;
  hasOlderLogs?: boolean;
  onTaskFilter?: (taskId: string | null) => void;
}

function LogRow({
  line,
  showAttrs,
  wrapLines,
  search,
  caseSensitive,
  isExpanded,
  onToggle,
  highlight,
  onTaskFilter,
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
  highlight?: boolean;
  onTaskFilter?: (taskId: string) => void;
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
      className={`hover:bg-muted/50 group ${jsonLine ? "cursor-pointer" : ""} ${highlight ? "bg-yellow-500/10" : ""}`}
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
          className="px-2 py-px whitespace-nowrap align-top font-mono"
          title={line.attrs?.taskId ? `Filter by task ${line.attrs.taskId.slice(0, 12)}` : undefined}
        >
          {line.attrs?.taskId && onTaskFilter ? (
            <button
              type="button"
              onClick={(e) => { e.stopPropagation(); onTaskFilter(line.attrs!.taskId!); }}
              className="text-muted-foreground/60 hover:text-primary hover:underline cursor-pointer"
            >
              {line.attrs.taskId.slice(0, 8)}
            </button>
          ) : (
            <span className="text-muted-foreground/60">{line.attrs?.taskId?.slice(0, 8)}</span>
          )}
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

export function LogTable({ containerRef, handleScroll, filtered, showAttrs, wrapLines, search, caseSensitive, highlightIndex, scrollToFiltered, loadingOlder, hasOlderLogs, onTaskFilter }: LogTableProps) {
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
            highlightIndex={highlightIndex}
            scrollToFiltered={scrollToFiltered}
            onTaskFilter={onTaskFilter}
          />
        ) : (
          <tbody>
            {loadingOlder && (
              <tr><td colSpan={showAttrs ? 5 : 4} className="text-center py-2 text-xs text-muted-foreground">
                Loading older logs...
              </td></tr>
            )}
            {!loadingOlder && hasOlderLogs === false && (
              <tr><td colSpan={showAttrs ? 5 : 4} className="text-center py-2 text-xs text-muted-foreground">
                Beginning of logs
              </td></tr>
            )}
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
                highlight={line.index === highlightIndex}
                onTaskFilter={onTaskFilter ?? undefined}
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
  highlightIndex,
  scrollToFiltered,
  onTaskFilter,
}: {
  containerRef: React.RefObject<HTMLDivElement | null>;
  filtered: LogLine[];
  showAttrs: boolean;
  wrapLines: boolean;
  search: string;
  caseSensitive: boolean;
  expanded: Set<number>;
  toggleExpanded: (index: number) => void;
  highlightIndex?: number;
  scrollToFiltered?: number;
  onTaskFilter?: (taskId: string | null) => void;
}) {
  const virtualizer = useVirtualizer({
    count: filtered.length,
    getScrollElement: () => containerRef.current,
    estimateSize: () => LOG_ROW_HEIGHT_ESTIMATE,
    overscan: 50,
  });

  useEffect(() => {
    if (scrollToFiltered !== undefined) {
      virtualizer.scrollToIndex(scrollToFiltered, { align: "center" });
    }
  }, [scrollToFiltered, virtualizer]);

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
            highlight={line.index === highlightIndex}
            onTaskFilter={onTaskFilter ?? undefined}
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
