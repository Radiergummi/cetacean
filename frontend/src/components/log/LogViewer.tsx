import { SectionToggle, useSectionCollapse } from "../CollapsibleSection";
import { Spinner } from "../Spinner";
import type { LogLine } from "./log-utils";
import { logLineKey } from "./log-utils";
import { LogTable } from "./LogTable";
import { LevelFilter, StreamFilterToggle, TimeRangeSelector, ToolbarButton } from "./LogToolbar";
import { useLogData } from "./useLogData";
import { useLogFilter } from "./useLogFilter";
import { useLogTimeRange } from "./useLogTimeRange";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  Copy,
  Download,
  FileText,
  Play,
  RefreshCw,
  Search,
  Square,
  Trash2,
  WrapText,
  X,
} from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

interface Props {
  serviceId?: string;
  taskId?: string;
  header?: React.ReactNode;
}

export default function LogViewer({ serviceId, taskId, header }: Props) {
  const logId = (serviceId || taskId)!;
  const isTask = !!taskId;

  const { open, toggle: toggleCollapse } = useSectionCollapse(header ? String(header) : "Logs");
  const [wrapLines, setWrapLines] = useState(() => matchMedia("(max-width: 767px)").matches);
  const [streamFilter, setStreamFilter] = useState<"all" | "stdout" | "stderr">("all");
  const [pinnedLines, setPinnedLines] = useState<LogLine[]>([]);
  const searchRef = useRef<HTMLInputElement>(null);

  const pinnedKeys = useMemo(() => new Set(pinnedLines.map(logLineKey)), [pinnedLines]);

  const handlePin = useCallback((line: LogLine) => {
    const key = logLineKey(line);
    setPinnedLines((prev) => {
      const idx = prev.findIndex((l) => logLineKey(l) === key);
      if (idx !== -1) return [...prev.slice(0, idx), ...prev.slice(idx + 1)];
      if (prev.length >= 3) return prev;
      return [...prev, line];
    });
  }, []);

  const { timeRange, updateTimeRange } = useLogTimeRange();
  const data = useLogData({ logId, isTask, timeRange, streamFilter });
  const {
    search,
    setSearch,
    caseSensitive,
    setCaseSensitive,
    matchIndex,
    setMatchIndex,
    useRegex,
    setUseRegex,
    levelFilter,
    setLevelFilter,
    taskFilter,
    setTaskFilter,
    filtered,
  } = useLogFilter(data.lines);

  const showAttrs = useMemo(
    () => !isTask && data.lines.some(({ attrs }) => attrs?.taskId),
    [isTask, data.lines],
  );

  // Keyboard shortcut: Ctrl+F to focus search
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key === "f" &&
        data.containerRef.current?.contains(document.activeElement)
      ) {
        event.preventDefault();
        searchRef.current?.focus();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [data.containerRef]);

  const formatLogs = useCallback(
    () =>
      filtered
        .map(({ message, timestamp }) => (timestamp ? `${timestamp} ${message}` : message))
        .join("\n"),
    [filtered],
  );

  const copyLogs = useCallback(() => {
    void navigator.clipboard.writeText(formatLogs());
  }, [formatLogs]);

  const downloadLogs = useCallback(() => {
    const blob = new Blob([formatLogs()], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `logs-${logId.slice(0, 12)}.log`;
    link.click();
    URL.revokeObjectURL(url);
  }, [formatLogs, logId]);

  const toggle = header ? (
    <SectionToggle
      title={header}
      open={open}
      onToggle={toggleCollapse}
      className="flex w-full cursor-pointer items-center gap-1.5 text-sm font-medium tracking-wider text-muted-foreground uppercase transition-colors hover:text-foreground sm:mr-auto sm:w-auto"
    />
  ) : null;

  if (!open) {
    return <div className="flex min-h-8 items-center">{toggle}</div>;
  }

  return (
    <div
      id="logs"
      className="flex flex-col gap-2"
    >
      <nav
        role="toolbar"
        className="flex min-h-8 flex-wrap items-center gap-1.5"
      >
        {toggle}

        {data.live && (
          <span className="me-2 flex items-center gap-1.5 text-xs text-green-500 opacity-100 transition starting:opacity-0">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-green-500" />
            Live
          </span>
        )}

        <TimeRangeSelector
          value={timeRange}
          onChange={(range) => {
            updateTimeRange(range);

            if (data.live) {
              data.stopLive();
            }
          }}
        />

        <ToolbarButton
          onClick={data.fetchLogs}
          title="Refresh"
          icon={<RefreshCw className="size-3.5" />}
        />
        <ToolbarButton
          onClick={data.toggleLive}
          title={data.live ? "Stop live" : "Live tail"}
          icon={data.live ? <Square className="size-3.5" /> : <Play className="size-3.5" />}
          active={data.live}
        />
        <ToolbarButton
          onClick={() => setWrapLines(!wrapLines)}
          title="Toggle wrap"
          icon={<WrapText className="size-3.5" />}
          active={wrapLines}
        />

        <div className="mx-0.5 hidden h-5 w-px bg-border md:block" />

        <StreamFilterToggle
          value={streamFilter}
          onChange={setStreamFilter}
        />
        <LevelFilter
          value={levelFilter}
          onChange={setLevelFilter}
        />

        <div className="mx-0.5 hidden h-5 w-px bg-border md:block" />

        <ToolbarButton
          onClick={copyLogs}
          title="Copy"
          icon={<Copy className="size-3.5" />}
        />
        <ToolbarButton
          onClick={downloadLogs}
          title="Download"
          icon={<Download className="size-3.5" />}
        />

        <div className="mx-0.5 hidden h-5 w-px bg-border md:block" />

        {/* Search */}
        <div className="relative flex items-center">
          <Search className="pointer-events-none absolute left-2 size-3.5 text-muted-foreground" />
          <input
            ref={searchRef}
            type="text"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Filter logs..."
            className="h-8 w-56 max-w-full rounded-md border bg-background pr-16 pl-7 font-mono text-xs"
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                setSearch("");
                return;
              }
              if (event.key === "Enter" && search && filtered.length > 0) {
                event.preventDefault();
                if (event.shiftKey) {
                  setMatchIndex((current) => (current - 1 + filtered.length) % filtered.length);
                } else {
                  setMatchIndex((current) => (current + 1) % filtered.length);
                }
              }
            }}
          />
          <div className="absolute right-1.5 flex items-center gap-0.5">
            <button
              onClick={() => setCaseSensitive(!caseSensitive)}
              aria-pressed={caseSensitive}
              className="rounded px-1 py-0.5 font-mono text-[10px] font-bold text-muted-foreground hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
              title="Case sensitive"
            >
              Aa
            </button>
            <button
              onClick={() => setUseRegex(!useRegex)}
              aria-pressed={useRegex}
              className="rounded px-1 py-0.5 font-mono text-[10px] font-bold text-muted-foreground hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
              title="Regex"
            >
              .*
            </button>
            {search && (
              <button
                onClick={() => setSearch("")}
                className="p-0.5 text-muted-foreground hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            )}
          </div>
        </div>

        {search && (
          <span className="text-xs text-muted-foreground tabular-nums">
            {filtered.length > 0 ? `${matchIndex + 1}/${filtered.length}` : "0/0"}
          </span>
        )}

        {/* Clear */}
        <ToolbarButton
          onClick={() => {
            data.setLines([]);
            setPinnedLines([]);
          }}
          title="Clear logs"
          icon={<Trash2 className="size-3.5" />}
        />

        {taskFilter && (
          <button
            onClick={() => setTaskFilter(null)}
            className="flex items-center gap-1 rounded-md border border-primary/30 bg-primary/10 px-2 py-1.5 text-xs text-primary hover:bg-primary/20"
            title="Clear task filter"
          >
            <span className="font-mono">{taskFilter.slice(0, 8)}</span>
            <X className="size-3" />
          </button>
        )}
      </nav>

      {/* Log area */}
      {data.loading ? (
        <div className="flex h-100 items-center justify-center rounded-lg border bg-muted/30 dark:border-gray-800 dark:bg-gray-950">
          <div className="flex flex-col items-center gap-3 text-muted-foreground">
            <Spinner className="size-6" />
            <p className="text-sm">Loading logs…</p>
          </div>
        </div>
      ) : data.error ? (
        <div className="flex h-100 items-center justify-center rounded-lg border bg-muted/30 dark:border-gray-800 dark:bg-gray-950">
          <div className="flex flex-col items-center gap-3 text-center">
            <AlertTriangle className="size-6 text-red-500 dark:text-red-400" />
            <div>
              <p className="mb-1 text-sm font-medium text-red-600 dark:text-red-400">
                Failed to load logs
              </p>
              <p className="mb-3 text-xs text-muted-foreground">{data.error}</p>
            </div>
            <button
              onClick={data.fetchLogs}
              className="rounded-md border px-4 py-1.5 text-sm hover:bg-muted"
            >
              Retry
            </button>
          </div>
        </div>
      ) : data.lines.length === 0 ? (
        <div className="flex h-100 items-center justify-center rounded-lg border bg-muted/30 dark:border-gray-800 dark:bg-gray-950">
          <div className="flex flex-col items-center gap-3 text-muted-foreground">
            <FileText className="size-6" />
            <p className="text-sm">No logs yet — the container hasn't produced any output</p>
          </div>
        </div>
      ) : filtered.length === 0 ? (
        <div className="flex h-100 items-center justify-center rounded-lg border bg-muted/30 dark:border-gray-800 dark:bg-gray-950">
          <p className="text-sm text-muted-foreground">No matching log lines</p>
        </div>
      ) : (
        <div className="relative">
          <LogTable
            containerRef={data.containerRef}
            handleScroll={data.handleScroll}
            filtered={filtered}
            showAttrs={showAttrs}
            wrapLines={wrapLines}
            search={search}
            caseSensitive={caseSensitive}
            useRegex={useRegex}
            highlightIndex={search && filtered.length > 0 ? filtered[matchIndex]?.index : undefined}
            scrollToFiltered={search && filtered.length > 0 ? matchIndex : undefined}
            following={data.following}
            onTaskFilter={setTaskFilter}
            pinnedKeys={pinnedKeys}
            pinnedLines={pinnedLines}
            onTogglePin={handlePin}
          />

          {data.atTop && data.hasOlderLogs && (
            <button
              onClick={data.loadOlder}
              disabled={data.loadingOlder}
              data-pinned={pinnedLines.length || undefined}
              className="absolute top-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-card px-3 py-1.5 text-xs text-foreground shadow-lg transition-colors hover:bg-muted data-[pinned='1']:top-8 data-[pinned='2']:top-13 data-[pinned='3']:top-18"
            >
              {data.loadingOlder ? <Spinner className="size-3" /> : <ArrowUp className="size-3" />}
              Load older
            </button>
          )}

          {!data.following ? (
            <button
              onClick={() => data.setFollowing(true)}
              className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-card px-3 py-1.5 text-xs text-foreground shadow-lg transition-colors hover:bg-muted"
            >
              <ArrowDown className="size-3" />
              Jump to bottom
            </button>
          ) : !data.live && data.hasNewerLogs ? (
            <button
              onClick={data.loadNewer}
              disabled={data.loadingNewer}
              className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full border bg-card px-3 py-1.5 text-xs text-foreground shadow-lg transition-colors hover:bg-muted"
            >
              Load newer
              {data.loadingNewer ? (
                <Spinner className="size-3" />
              ) : (
                <ArrowDown className="size-3" />
              )}
            </button>
          ) : null}
        </div>
      )}
    </div>
  );
}
