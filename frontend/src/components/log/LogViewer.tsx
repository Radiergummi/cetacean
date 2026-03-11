import {
    AlertTriangle,
    ArrowDown,
    ArrowUp,
    ChevronRight,
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
import {type default as React, useCallback, useEffect, useMemo, useRef, useState} from "react";
import {Spinner} from "../Spinner";
import {LogTable} from "./LogTable";
import type {LogLine} from "./log-utils";
import {logLineKey} from "./log-utils";
import {LevelFilter, StreamFilterToggle, TimeRangeSelector, ToolbarButton} from "./LogToolbar";
import {useLogData} from "./useLogData";
import {useLogFilter} from "./useLogFilter";
import {useLogTimeRange} from "./useLogTimeRange";

interface Props {
    serviceId?: string;
    taskId?: string;
    header?: React.ReactNode;
}

export default function LogViewer({serviceId, taskId, header}: Props) {
    const logId = (
        serviceId || taskId
    )!;
    const isTask = !!taskId;

    const [collapsed, setCollapsed] = useState(false);
    const [wrapLines, setWrapLines] = useState(false);
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

    const {timeRange, updateTimeRange} = useLogTimeRange();
    const data = useLogData({logId, isTask, timeRange, streamFilter});
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
    } = useLogFilter(data.lines, streamFilter);

    const showAttrs = !isTask && data.lines.some(({attrs}) => attrs?.taskId);

    // Keyboard shortcut: Ctrl+F to focus search
    useEffect(() => {
        const handler = (event: KeyboardEvent) => {
            if (
                (
                    event.metaKey || event.ctrlKey
                ) &&
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

    const copyLogs = () => {
        const text = filtered
            .map(({message, timestamp}) => (
                timestamp ? `${timestamp} ${message}` : message
            ))
            .join("\n");
        void navigator.clipboard.writeText(text);
    };

    const downloadLogs = () => {
        const text = filtered
            .map(({message, timestamp}) => (
                timestamp ? `${timestamp} ${message}` : message
            ))
            .join("\n");
        const blob = new Blob([text], {type: "text/plain"});
        const url = URL.createObjectURL(blob);
        const link = document.createElement("a");
        link.href = url;
        link.download = `logs-${logId.slice(0, 12)}.log`;
        link.click();
        URL.revokeObjectURL(url);
    };

    const toggle = header ? (
        <button
            type="button"
            onClick={() => setCollapsed((previous) => !previous)}
            className="flex items-center gap-1.5 text-sm font-medium uppercase tracking-wider text-muted-foreground
            hover:text-foreground transition-colors cursor-pointer mr-auto"
        >
            <ChevronRight data-open={!collapsed || undefined} className="h-4 w-4 transition-transform data-open:rotate-90"/>
            {header}
        </button>
    ) : null;

    if (collapsed) {
        return <div className="min-h-8 flex items-center">{toggle}</div>;
    }

    return (
        <div id="logs" className="flex flex-col gap-2">
            <nav role="toolbar" className="flex flex-wrap items-center gap-1.5 min-h-8">
                {toggle}

                {data.live && (
                    <span className="flex items-center gap-1.5 text-xs text-green-500 me-2 starting:opacity-0 opacity-100 transition">
                        <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse"/>
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
                    icon={<RefreshCw className="size-3.5"/>}
                />
                <ToolbarButton
                    onClick={data.toggleLive}
                    title={data.live ? "Stop live" : "Live tail"}
                    icon={data.live ? <Square className="size-3.5"/> : <Play className="size-3.5"/>}
                    active={data.live}
                />
                <ToolbarButton
                    onClick={() => setWrapLines(!wrapLines)}
                    title="Toggle wrap"
                    icon={<WrapText className="size-3.5"/>}
                    active={wrapLines}
                />

                <div className="w-px h-5 bg-border mx-0.5"/>

                <StreamFilterToggle value={streamFilter} onChange={setStreamFilter}/>
                <LevelFilter value={levelFilter} onChange={setLevelFilter}/>

                <div className="w-px h-5 bg-border mx-0.5"/>

                <ToolbarButton onClick={copyLogs} title="Copy" icon={<Copy className="size-3.5"/>}/>
                <ToolbarButton
                    onClick={downloadLogs}
                    title="Download"
                    icon={<Download className="size-3.5"/>}
                />

                <div className="w-px h-5 bg-border mx-0.5"/>

                {/* Search */}
                <div className="relative flex items-center">
                    <Search className="size-3.5 absolute left-2 text-muted-foreground pointer-events-none"/>
                    <input
                        ref={searchRef}
                        type="text"
                        value={search}
                        onChange={(event) => setSearch(event.target.value)}
                        placeholder="Filter logs..."
                        className="h-8 pl-7 pr-16 text-xs border rounded-md bg-background font-mono w-56"
                        onKeyDown={(event) => {
                            if (event.key === "Escape") {
                                setSearch("");
                                return;
                            }
                            if (event.key === "Enter" && search && filtered.length > 0) {
                                event.preventDefault();
                                if (event.shiftKey) {
                                    setMatchIndex((current) => (
                                        current - 1 + filtered.length
                                    ) % filtered.length);
                                } else {
                                    setMatchIndex((current) => (
                                        current + 1
                                    ) % filtered.length);
                                }
                            }
                        }}
                    />
                    <div className="absolute right-1.5 flex items-center gap-0.5">
                        <button
                            onClick={() => setCaseSensitive(!caseSensitive)}
                            aria-pressed={caseSensitive}
                            className="px-1 py-0.5 text-[10px] rounded font-mono font-bold text-muted-foreground hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
                            title="Case sensitive"
                        >
                            Aa
                        </button>
                        <button
                            onClick={() => setUseRegex(!useRegex)}
                            aria-pressed={useRegex}
                            className="px-1 py-0.5 text-[10px] rounded font-mono font-bold text-muted-foreground hover:text-foreground aria-pressed:bg-primary aria-pressed:text-primary-foreground"
                            title="Regex"
                        >
                            .*
                        </button>
                        {search && (
                            <button
                                onClick={() => setSearch("")}
                                className="text-muted-foreground hover:text-foreground p-0.5"
                            >
                                <X className="size-3"/>
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
                    onClick={() => { data.setLines([]); setPinnedLines([]); }}
                    title="Clear logs"
                    icon={<Trash2 className="size-3.5"/>}
                />

                {taskFilter && (
                    <button
                        onClick={() => setTaskFilter(null)}
                        className="flex items-center gap-1 px-2 py-1.5 text-xs rounded-md bg-primary/10 border border-primary/30 text-primary hover:bg-primary/20"
                        title="Clear task filter"
                    >
                        <span className="font-mono">{taskFilter.slice(0, 8)}</span>
                        <X className="size-3"/>
                    </button>
                )}
            </nav>

            {/* Log area */}
            {data.loading ? (
                <div className="log-panel flex items-center justify-center">
                    <div className="flex flex-col items-center gap-3 text-muted-foreground">
                        <Spinner className="size-6"/>
                        <p className="text-sm">Loading logs…</p>
                    </div>
                </div>
            ) : data.error ? (
                <div className="log-panel flex items-center justify-center">
                    <div className="flex flex-col items-center gap-3 text-center">
                        <AlertTriangle className="size-6 text-red-500 dark:text-red-400"/>
                        <div>
                            <p className="text-red-600 dark:text-red-400 text-sm font-medium mb-1">
                                Failed to load logs
                            </p>
                            <p className="text-muted-foreground text-xs mb-3">{data.error}</p>
                        </div>
                        <button
                            onClick={data.fetchLogs}
                            className="px-4 py-1.5 text-sm rounded-md border hover:bg-muted"
                        >
                            Retry
                        </button>
                    </div>
                </div>
            ) : data.lines.length === 0 ? (
                <div className="log-panel flex items-center justify-center">
                    <div className="flex flex-col items-center gap-3 text-muted-foreground">
                        <FileText className="size-6"/>
                        <p className="text-sm">No logs yet — the container hasn't produced any output</p>
                    </div>
                </div>
            ) : filtered.length === 0 ? (
                <div className="log-panel flex items-center justify-center">
                    <p className="text-muted-foreground text-sm">No matching log lines</p>
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
                        onPin={handlePin}
                        onUnpin={handlePin}
                    />

                    {data.atTop && data.hasOlderLogs && (
                        <button
                            onClick={data.loadOlder}
                            disabled={data.loadingOlder}
                            data-pinned={pinnedLines.length || undefined}
                            className="absolute top-3 data-[pinned='1']:top-8 data-[pinned='2']:top-13 data-[pinned='3']:top-18 left-1/2 -translate-x-1/2 flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-full bg-card text-foreground border shadow-lg hover:bg-muted transition-colors"
                        >
                            {data.loadingOlder ? <Spinner className="size-3"/> : <ArrowUp className="size-3"/>}
                            Load older
                        </button>
                    )}

                    {!data.following ? (
                        <button
                            onClick={() => data.setFollowing(true)}
                            className="absolute bottom-3 left-1/2 -translate-x-1/2 flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-full bg-card text-foreground border shadow-lg hover:bg-muted transition-colors"
                        >
                            <ArrowDown className="size-3"/>
                            Jump to bottom
                        </button>
                    ) : !data.live && data.hasNewerLogs ? (
                        <button
                            onClick={data.loadNewer}
                            disabled={data.loadingNewer}
                            className="absolute bottom-3 left-1/2 -translate-x-1/2 flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-full bg-card text-foreground border shadow-lg hover:bg-muted transition-colors"
                        >
                            Load newer
                            {data.loadingNewer ? <Spinner className="size-3"/> : <ArrowDown className="size-3"/>}
                        </button>
                    ) : null}
                </div>
            )}
        </div>
    );
}
