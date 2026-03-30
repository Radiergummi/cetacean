import { SectionToggle, useSectionCollapse } from "../CollapsibleSection";
import type { LogLine } from "./log-utils";
import { logLineKey } from "./log-utils";
import { LogEmptyState } from "./LogEmptyState";
import { LogOverlays } from "./LogOverlays";
import { LogSearch } from "./LogSearch";
import { LogTable } from "./LogTable";
import { LevelFilter, StreamFilterToggle, TimeRangeSelector, ToolbarButton } from "./LogToolbar";
import { useLogData } from "./useLogData";
import { useLogFilter } from "./useLogFilter";
import { useLogTimeRange } from "./useLogTimeRange";
import {
  Copy,
  Download,
  GripHorizontal,
  Maximize,
  Minimize,
  Play,
  RefreshCw,
  Square,
  Trash2,
  WrapText,
  X,
} from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

const defaultHeight = 400;
const minHeight = 150;

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
  const [height, setHeight] = useState(defaultHeight);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  const pinnedKeys = useMemo(() => new Set(pinnedLines.map(logLineKey)), [pinnedLines]);

  const handlePin = useCallback((line: LogLine) => {
    const key = logLineKey(line);

    setPinnedLines((previous) => {
      const idx = previous.findIndex((l) => logLineKey(l) === key);

      if (idx !== -1) {
        return [...previous.slice(0, idx), ...previous.slice(idx + 1)];
      }

      if (previous.length >= 3) {
        return previous;
      }

      return [...previous, line];
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

  // Sync fullscreen state with browser
  useEffect(() => {
    function handleChange() {
      setIsFullscreen(document.fullscreenElement === containerRef.current);
    }

    document.addEventListener("fullscreenchange", handleChange);

    return () => document.removeEventListener("fullscreenchange", handleChange);
  }, []);

  function toggleFullscreen() {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void containerRef.current?.requestFullscreen();
    }
  }

  // Resize handle drag — auto-scrolls the page to keep the handle under the cursor
  const handleResizeStart = useCallback(
    (event: React.PointerEvent) => {
      event.preventDefault();
      const startY = event.clientY;
      const startHeight = height;
      const startScroll = window.scrollY;
      const pointerId = event.pointerId;
      const target = event.currentTarget as HTMLElement;
      target.setPointerCapture(pointerId);

      function onMove(moveEvent: PointerEvent) {
        const delta = moveEvent.clientY - startY + (window.scrollY - startScroll);
        setHeight(Math.max(minHeight, startHeight + delta));
        target.scrollIntoView({ block: "nearest" });
      }

      function onUp() {
        target.releasePointerCapture(pointerId);
        target.removeEventListener("pointermove", onMove);
        target.removeEventListener("pointerup", onUp);
      }

      target.addEventListener("pointermove", onMove);
      target.addEventListener("pointerup", onUp);
    },
    [height],
  );

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
      className="flex w-full cursor-pointer items-center gap-1.5 text-sm font-medium tracking-wider text-muted-foreground uppercase transition-colors hover:text-foreground sm:me-auto sm:w-auto"
    />
  ) : null;

  if (!open) {
    return <div className="flex min-h-8 items-center">{toggle}</div>;
  }

  const hasContent = !data.loading && !data.error && data.lines.length > 0 && filtered.length > 0;

  const toolbar = (
    <nav
      role="toolbar"
      className={
        isFullscreen
          ? "flex min-h-10 flex-wrap items-center gap-1.5 border-b bg-background p-3"
          : "mb-2 flex min-h-8 flex-wrap items-center gap-1.5"
      }
    >
      {!isFullscreen && toggle}

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

      <LogSearch
        search={search}
        setSearch={setSearch}
        caseSensitive={caseSensitive}
        setCaseSensitive={setCaseSensitive}
        useRegex={useRegex}
        setUseRegex={setUseRegex}
        matchIndex={matchIndex}
        setMatchIndex={setMatchIndex}
        matchCount={filtered.length}
        searchRef={searchRef}
        logContainerRef={data.containerRef}
      />

      {/* Clear */}
      <ToolbarButton
        onClick={() => {
          data.setLines([]);
          setPinnedLines([]);
        }}
        title="Clear logs"
        icon={<Trash2 className="size-3.5" />}
      />

      {!isFullscreen && (
        <ToolbarButton
          onClick={toggleFullscreen}
          title="Fullscreen"
          icon={<Maximize className="size-3.5" />}
        />
      )}

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

      {isFullscreen && (
        <ToolbarButton
          onClick={toggleFullscreen}
          title="Exit Fullscreen"
          className="ms-auto"
          icon={<Minimize className="size-3.5" />}
        />
      )}
    </nav>
  );

  return (
    <div
      ref={containerRef}
      id="logs"
      className={isFullscreen ? "flex h-full flex-col bg-background" : "flex flex-col gap-1"}
    >
      {toolbar}

      {!hasContent ? (
        <LogEmptyState
          loading={data.loading}
          error={data.error}
          hasLines={data.lines.length > 0}
          hasFiltered={filtered.length > 0}
          onRetry={data.fetchLogs}
          className={
            isFullscreen
              ? "flex flex-1 items-center justify-center bg-muted/30 dark:bg-gray-950"
              : "flex items-center justify-center rounded-lg border bg-muted/30 dark:border-gray-800 dark:bg-gray-950"
          }
          style={isFullscreen ? undefined : { height }}
        />
      ) : (
        <div className={isFullscreen ? "relative flex min-h-0 flex-1 flex-col" : "relative"}>
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
            height={height}
            fillHeight={isFullscreen}
          />

          <LogOverlays
            atTop={data.atTop}
            hasOlderLogs={data.hasOlderLogs}
            loadOlder={data.loadOlder}
            loadingOlder={data.loadingOlder}
            pinnedCount={pinnedLines.length}
            following={data.following}
            setFollowing={data.setFollowing}
            live={data.live}
            hasNewerLogs={data.hasNewerLogs}
            loadNewer={data.loadNewer}
            loadingNewer={data.loadingNewer}
          />
        </div>
      )}

      {!isFullscreen && (
        <div
          onPointerDown={handleResizeStart}
          onDoubleClick={() => setHeight(defaultHeight)}
          className="group flex h-2.5 cursor-row-resize items-center justify-center rounded-xs bg-muted/60 hover:bg-muted"
        >
          <GripHorizontal className="size-2.5 text-muted-foreground/50 group-hover:text-muted-foreground" />
        </div>
      )}
    </div>
  );
}
