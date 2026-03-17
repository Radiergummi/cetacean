import type { LogLine } from "./log-utils";
import {
  formatTime,
  isJSON,
  LEVEL_BAR,
  LOG_ROW_HEIGHT_ESTIMATE,
  LOG_VIRTUAL_THRESHOLD,
  logLineKey,
} from "./log-utils";
import { LogMessage } from "./LogMessage";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Pin, PinOff } from "lucide-react";
import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

export interface LogTableProps {
  containerRef: RefObject<HTMLDivElement | null>;
  handleScroll: () => void;
  filtered: LogLine[];
  showAttrs: boolean;
  wrapLines: boolean;
  search: string;
  caseSensitive: boolean;
  useRegex: boolean;
  highlightIndex?: number;
  scrollToFiltered?: number;
  following?: boolean;
  onTaskFilter?: (taskId: string | null) => void;
  pinnedKeys?: Set<string>;
  pinnedLines?: LogLine[];
  onTogglePin?: (line: LogLine) => void;
}

function LogRow({
  line,
  showAttrs,
  wrapLines,
  search,
  caseSensitive,
  useRegex,
  isExpanded,
  onToggle,
  highlight,
  onTaskFilter,
  measureRef,
  dataIndex,
  isPinned,
  onTogglePin,
}: {
  line: LogLine;
  showAttrs: boolean;
  wrapLines: boolean;
  search: string;
  caseSensitive: boolean;
  useRegex: boolean;
  isExpanded: boolean;
  onToggle: ((index: number) => void) | undefined;
  highlight?: boolean;
  onTaskFilter?: (taskId: string) => void;
  measureRef?: (el: HTMLElement | null) => void;
  dataIndex?: number;
  isPinned?: boolean;
  onTogglePin?: (line: LogLine) => void;
}) {
  const jsonLine = isJSON(line.message);
  const prettyPrint = jsonLine && (wrapLines || isExpanded);
  const PinIcon = isPinned ? PinOff : Pin;
  return (
    <tr
      key={line.index}
      ref={measureRef}
      data-index={dataIndex}
      data-json={jsonLine ? "" : undefined}
      data-highlight={highlight || undefined}
      className="group hover:bg-muted/50 data-highlight:bg-yellow-500/10 data-json:cursor-pointer"
      onClick={onToggle && jsonLine ? () => onToggle(line.index) : undefined}
    >
      <td className={`${onTogglePin ? "w-6" : "w-0.75"} align-stretch relative ps-0.75`}>
        <div
          className={`min-h-full w-0.75 ${LEVEL_BAR[line.level]} absolute top-0 bottom-0 left-0`}
        />
        {onTogglePin && (
          <button
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              onTogglePin(line);
            }}
            aria-pressed={isPinned || undefined}
            className="flex items-center justify-center p-1 text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-foreground aria-pressed:text-primary aria-pressed:opacity-100"
            title={isPinned ? "Unpin line" : "Pin line"}
          >
            <PinIcon className="size-2.5" />
          </button>
        )}
      </td>
      <td
        className="py-px pe-2 align-top whitespace-nowrap text-muted-foreground select-all"
        title={line.timestamp}
      >
        {formatTime(line.timestamp)}
      </td>
      {showAttrs && (
        <td
          className="py-px pe-2 align-top font-mono whitespace-nowrap"
          title={
            line.attrs?.taskId ? `Filter by task ${line.attrs.taskId.slice(0, 12)}` : undefined
          }
        >
          {line.attrs?.taskId && onTaskFilter ? (
            <button
              type="button"
              onClick={(event) => {
                event.stopPropagation();

                onTaskFilter(line.attrs!.taskId!);
              }}
              className="cursor-pointer text-muted-foreground/60 hover:text-primary hover:underline"
            >
              {line.attrs.taskId.slice(0, 8).trim()}
            </button>
          ) : (
            <span className="text-muted-foreground/60">
              {line.attrs?.taskId?.slice(0, 8).trim()}
            </span>
          )}
        </td>
      )}
      <td
        data-wrap={wrapLines || isExpanded || undefined}
        className="py-px pe-2 whitespace-pre text-foreground data-wrap:break-all data-wrap:whitespace-pre-wrap"
      >
        <LogMessage
          line={line}
          search={search}
          caseSensitive={caseSensitive}
          useRegex={useRegex}
          prettyJson={prettyPrint}
        />
      </td>
    </tr>
  );
}

export function LogTable({
  containerRef,
  handleScroll,
  filtered,
  showAttrs,
  wrapLines,
  search,
  caseSensitive,
  useRegex,
  highlightIndex,
  scrollToFiltered,
  following,
  onTaskFilter,
  pinnedKeys,
  pinnedLines,
  onTogglePin,
}: LogTableProps) {
  const [expanded, setExpanded] = useState<Set<number>>(() => new Set());
  const useVirtual = filtered.length > LOG_VIRTUAL_THRESHOLD;

  const toggleExpanded = useCallback((index: number) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(index)) {
        next.delete(index);
      } else {
        next.add(index);
      }
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
        {pinnedLines && pinnedLines.length > 0 && (
          <thead className="sticky top-0 z-10 border-b bg-muted/80 shadow-md backdrop-blur-sm">
            {pinnedLines.map((line) => (
              <tr
                key={logLineKey(line)}
                onClick={() => onTogglePin?.(line)}
                className="cursor-pointer hover:bg-muted/50"
              >
                <td className="align-stretch relative w-6 ps-1.75">
                  <div
                    className={`min-h-full w-0.75 ${LEVEL_BAR[line.level]} absolute top-0 bottom-0 left-0`}
                  />
                  <PinOff className="size-2.5 text-muted-foreground" />
                </td>
                <td
                  className="py-px pe-2 align-top whitespace-nowrap text-muted-foreground"
                  title={line.timestamp}
                >
                  {formatTime(line.timestamp)}
                </td>
                {showAttrs && (
                  <td className="py-px pe-2 align-top font-mono whitespace-nowrap">
                    <span className="text-muted-foreground/60">
                      {line.attrs?.taskId?.slice(0, 8)}
                    </span>
                  </td>
                )}
                <td className="overflow-hidden py-px pe-2 text-ellipsis whitespace-pre text-foreground">
                  {line.message}
                </td>
              </tr>
            ))}
          </thead>
        )}
        {useVirtual ? (
          <VirtualLogBody
            containerRef={containerRef}
            filtered={filtered}
            showAttrs={showAttrs}
            wrapLines={wrapLines}
            search={search}
            caseSensitive={caseSensitive}
            useRegex={useRegex}
            expanded={expanded}
            toggleExpanded={toggleExpanded}
            highlightIndex={highlightIndex}
            scrollToFiltered={scrollToFiltered}
            following={following}
            onTaskFilter={onTaskFilter}
            pinnedKeys={pinnedKeys}
            onTogglePin={onTogglePin}
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
                useRegex={useRegex}
                isExpanded={expanded.has(line.index)}
                onToggle={toggleExpanded}
                highlight={line.index === highlightIndex}
                onTaskFilter={onTaskFilter ?? undefined}
                isPinned={pinnedKeys?.has(logLineKey(line))}
                onTogglePin={onTogglePin}
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
  useRegex,
  expanded,
  toggleExpanded,
  highlightIndex,
  scrollToFiltered,
  following,
  onTaskFilter,
  pinnedKeys,
  onTogglePin,
}: {
  containerRef: RefObject<HTMLDivElement | null>;
  filtered: LogLine[];
  showAttrs: boolean;
  wrapLines: boolean;
  search: string;
  caseSensitive: boolean;
  useRegex: boolean;
  expanded: Set<number>;
  toggleExpanded: (index: number) => void;
  highlightIndex?: number;
  scrollToFiltered?: number;
  following?: boolean;
  onTaskFilter?: (taskId: string | null) => void;
  pinnedKeys?: Set<string>;
  onTogglePin?: (line: LogLine) => void;
}) {
  // Track when the scroll container ref becomes available so we can
  // force a re-render — the virtualizer gets null from getScrollElement
  // on the first render (ref attaches during commit, not render phase).
  const [, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  const virtualizer = useVirtualizer({
    count: filtered.length,
    getScrollElement: () => containerRef.current,
    estimateSize: () => LOG_ROW_HEIGHT_ESTIMATE,
    overscan: 50,
  });

  // Scroll to bottom on initial mount when following.
  const didInitialScroll = useRef(false);
  useEffect(() => {
    if (!didInitialScroll.current && following && filtered.length > 0) {
      didInitialScroll.current = true;
      virtualizer.scrollToIndex(filtered.length - 1, { align: "end" });
    }
  }, [filtered.length, following, virtualizer]);

  useEffect(() => {
    if (scrollToFiltered !== undefined) {
      virtualizer.scrollToIndex(scrollToFiltered, { align: "center" });
    }
  }, [scrollToFiltered, virtualizer]);

  const virtualItems = virtualizer.getVirtualItems();
  const totalSize = virtualizer.getTotalSize();
  const colCount = showAttrs ? 4 : 3;

  return (
    <tbody>
      {virtualItems.length > 0 && (
        <tr>
          <td
            style={{ height: virtualItems[0].start, padding: 0 }}
            colSpan={colCount}
          />
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
            useRegex={useRegex}
            isExpanded={expanded.has(line.index)}
            onToggle={toggleExpanded}
            highlight={line.index === highlightIndex}
            onTaskFilter={onTaskFilter ?? undefined}
            measureRef={virtualizer.measureElement}
            dataIndex={virtualRow.index}
            isPinned={pinnedKeys?.has(logLineKey(line))}
            onTogglePin={onTogglePin}
          />
        );
      })}

      {virtualItems.length > 0 && (
        <tr>
          <td
            style={{
              height: Math.max(0, totalSize - virtualItems[virtualItems.length - 1].end),
              padding: 0,
            }}
            colSpan={colCount}
          />
        </tr>
      )}
    </tbody>
  );
}
