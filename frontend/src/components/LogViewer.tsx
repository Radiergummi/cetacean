import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import {
  Copy,
  Download,
  RefreshCw,
  WrapText,
  ArrowDown,
  Search,
  X,
  Play,
  Square,
  ChevronRight,
  AlertTriangle,
  Loader2,
  FileText,
} from "lucide-react";
import { api } from "../api/client";
import type { LogLine as ApiLogLine } from "../api/client";
import type { LogLine, TimeRange } from "./log-utils";
import { LIMIT_OPTIONS, MAX_LIVE_LINES, toLogLine } from "./log-utils";
import { LogTable } from "./LogTable";
import { TimeRangeSelector, StreamFilterToggle, ToolbarButton } from "./LogToolbar";

interface Props {
  serviceId?: string;
  taskId?: string;
  header?: React.ReactNode;
}

export default function LogViewer({ serviceId, taskId, header }: Props) {
  const logId = (serviceId || taskId)!;
  const isTask = !!taskId;
  const [collapsed, setCollapsed] = useState(false);
  const [lines, setLines] = useState<LogLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [limit, setLimit] = useState<number>(500);
  const [search, setSearch] = useState("");
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [useRegex, setUseRegex] = useState(false);
  const [streamFilter, setStreamFilter] = useState<"all" | "stdout" | "stderr">("all");
  const [wrapLines, setWrapLines] = useState(false);
  const [following, setFollowing] = useState(true);
  const [live, setLive] = useState(false);
  const [timeRange, setTimeRange] = useState<TimeRange>({ label: "All" });
  const containerRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const streamParam = streamFilter === "all" ? undefined : streamFilter;

  const fetchLogs = useCallback(() => {
    setLoading(true);
    setError(null);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 15_000);
    const opts = { limit, after: timeRange.since, before: timeRange.until, stream: streamParam, signal: controller.signal };
    const req = isTask ? api.taskLogs(logId, opts) : api.serviceLogs(logId, opts);
    req
      .then((resp) => {
        setLines((resp.lines ?? []).map(toLogLine));
        setLoading(false);
      })
      .catch((err) => {
        if (controller.signal.aborted) {
          setError("Request timed out");
        } else {
          setError(err instanceof Error ? err.message : "Failed to load logs");
        }
        setLoading(false);
      })
      .finally(() => clearTimeout(timeout));
  }, [logId, isTask, limit, timeRange, streamParam]);

  useEffect(() => {
    fetchLogs();
  }, [fetchLogs]);

  // Live streaming via SSE
  useEffect(() => {
    if (!live) return;

    const lastTs = lines.length > 0 ? lines[lines.length - 1].timestamp : undefined;
    const after = lastTs || new Date().toISOString();
    const streamOpts = { after, stream: streamParam };
    const url = isTask
      ? api.taskLogsStreamURL(logId, streamOpts)
      : api.serviceLogsStreamURL(logId, streamOpts);

    const es = new EventSource(url);
    abortRef.current = { abort: () => es.close() } as AbortController;

    es.onmessage = (event) => {
      try {
        const parsed: ApiLogLine = JSON.parse(event.data);
        setLines((current) => {
          const next = [...current, toLogLine(parsed, current.length)];
          return next.length > MAX_LIVE_LINES ? next.slice(-MAX_LIVE_LINES) : next;
        });
      } catch {
        // skip malformed events
      }
    };

    es.onerror = () => {
      // EventSource auto-reconnects on transient errors
    };

    return () => {
      es.close();
      abortRef.current = null;
    };
  }, [live, logId, isTask, streamParam]);

  // Auto-scroll to bottom when following (scroll within the container, not the page)
  useEffect(() => {
    if (following && containerRef.current) {
      const el = containerRef.current;
      el.scrollTop = el.scrollHeight;
    }
  }, [lines, following]);

  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
    setFollowing(atBottom);
  }, []);

  const showAttrs = !isTask && lines.some((l) => l.attrs?.taskId);

  const filtered = useMemo(() => {
    let result = lines;
    if (streamFilter !== "all") {
      result = result.filter((l) => l.stream === streamFilter);
    }
    if (search) {
      if (useRegex) {
        try {
          const re = new RegExp(search, caseSensitive ? "g" : "gi");
          result = result.filter((l) => re.test(l.message));
        } catch {
          // invalid regex, fall through to literal match
          const q = caseSensitive ? search : search.toLowerCase();
          result = result.filter((l) =>
            (caseSensitive ? l.message : l.message.toLowerCase()).includes(q),
          );
        }
      } else {
        const q = caseSensitive ? search : search.toLowerCase();
        result = result.filter((l) =>
          (caseSensitive ? l.message : l.message.toLowerCase()).includes(q),
        );
      }
    }
    return result;
  }, [lines, search, caseSensitive, useRegex, streamFilter]);

  const copyLogs = () => {
    const text = filtered
      .map((l) => (l.timestamp ? `${l.timestamp} ${l.message}` : l.message))
      .join("\n");
    navigator.clipboard.writeText(text);
  };

  const downloadLogs = () => {
    const text = filtered
      .map((l) => (l.timestamp ? `${l.timestamp} ${l.message}` : l.message))
      .join("\n");
    const blob = new Blob([text], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `logs-${logId.slice(0, 12)}.log`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const toggleLive = () => {
    if (live) {
      abortRef.current?.abort();
      setLive(false);
    } else {
      setFollowing(true);
      setLive(true);
    }
  };

  // Keyboard shortcut: Ctrl+F to focus search
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (
        (e.metaKey || e.ctrlKey) &&
        e.key === "f" &&
        containerRef.current?.contains(document.activeElement)
      ) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, []);

  const toggle = header ? (
    <button
      type="button"
      onClick={() => setCollapsed((c) => !c)}
      className="flex items-center gap-1.5 text-sm font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground transition-colors cursor-pointer mr-auto"
    >
      <ChevronRight className={`h-4 w-4 transition-transform ${collapsed ? "" : "rotate-90"}`} />
      {header}
    </button>
  ) : null;

  if (collapsed) {
    return <div className="min-h-8 flex items-center">{toggle}</div>;
  }

  return (
    <div className="flex flex-col gap-2">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-1.5 min-h-8">
        {toggle}
        <select
          value={limit}
          onChange={(e) => setLimit(Number(e.target.value))}
          className="h-8 px-2 text-xs border rounded-md bg-background"
        >
          {LIMIT_OPTIONS.map((n) => (
            <option key={n} value={n}>
              {n} lines
            </option>
          ))}
        </select>

        <TimeRangeSelector
          value={timeRange}
          onChange={(tr) => {
            setTimeRange(tr);
            if (live) {
              abortRef.current?.abort();
              setLive(false);
            }
          }}
        />

        <ToolbarButton
          onClick={fetchLogs}
          title="Refresh"
          icon={<RefreshCw className="w-3.5 h-3.5" />}
        />
        <ToolbarButton
          onClick={toggleLive}
          title={live ? "Stop live" : "Live tail"}
          icon={live ? <Square className="w-3.5 h-3.5" /> : <Play className="w-3.5 h-3.5" />}
          active={live}
        />
        <ToolbarButton
          onClick={() => setWrapLines(!wrapLines)}
          title="Toggle wrap"
          icon={<WrapText className="w-3.5 h-3.5" />}
          active={wrapLines}
        />

        <div className="w-px h-5 bg-border mx-0.5" />

        <StreamFilterToggle value={streamFilter} onChange={setStreamFilter} />

        <div className="w-px h-5 bg-border mx-0.5" />

        <ToolbarButton onClick={copyLogs} title="Copy" icon={<Copy className="w-3.5 h-3.5" />} />
        <ToolbarButton
          onClick={downloadLogs}
          title="Download"
          icon={<Download className="w-3.5 h-3.5" />}
        />

        <div className="w-px h-5 bg-border mx-0.5" />

        {/* Search */}
        <div className="relative flex items-center">
          <Search className="w-3.5 h-3.5 absolute left-2 text-muted-foreground pointer-events-none" />
          <input
            ref={searchRef}
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter logs..."
            className="h-8 pl-7 pr-16 text-xs border rounded-md bg-background font-mono w-56"
            onKeyDown={(e) => {
              if (e.key === "Escape") setSearch("");
            }}
          />
          <div className="absolute right-1.5 flex items-center gap-0.5">
            <button
              onClick={() => setCaseSensitive(!caseSensitive)}
              className={`px-1 py-0.5 text-[10px] rounded font-mono font-bold ${caseSensitive ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}
              title="Case sensitive"
            >
              Aa
            </button>
            <button
              onClick={() => setUseRegex(!useRegex)}
              className={`px-1 py-0.5 text-[10px] rounded font-mono font-bold ${useRegex ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground"}`}
              title="Regex"
            >
              .*
            </button>
            {search && (
              <button
                onClick={() => setSearch("")}
                className="text-muted-foreground hover:text-foreground p-0.5"
              >
                <X className="w-3 h-3" />
              </button>
            )}
          </div>
        </div>

        {search && (
          <span className="text-xs text-muted-foreground tabular-nums">
            {filtered.length}/{lines.length}
          </span>
        )}

        {/* Clear */}
        <ToolbarButton
          onClick={() => setLines([])}
          title="Clear logs"
          icon={<X className="w-3.5 h-3.5" />}
        />

        {live && (
          <span className="flex items-center gap-1.5 text-xs text-green-500">
            <span className="w-1.5 h-1.5 rounded-full bg-green-500 animate-pulse" />
            Live
          </span>
        )}
      </div>

      {/* Log area */}
      {loading ? (
        <div className="log-panel flex items-center justify-center">
          <div className="flex flex-col items-center gap-3 text-muted-foreground">
            <Loader2 className="w-6 h-6 animate-spin" />
            <p className="text-sm">Loading logs...</p>
          </div>
        </div>
      ) : error ? (
        <div className="log-panel flex items-center justify-center">
          <div className="flex flex-col items-center gap-3 text-center">
            <AlertTriangle className="w-6 h-6 text-red-500 dark:text-red-400" />
            <div>
              <p className="text-red-600 dark:text-red-400 text-sm font-medium mb-1">Failed to load logs</p>
              <p className="text-muted-foreground text-xs mb-3">{error}</p>
            </div>
            <button
              onClick={fetchLogs}
              className="px-4 py-1.5 text-sm rounded-md border hover:bg-muted"
            >
              Retry
            </button>
          </div>
        </div>
      ) : lines.length === 0 ? (
        <div className="log-panel flex items-center justify-center">
          <div className="flex flex-col items-center gap-3 text-muted-foreground">
            <FileText className="w-6 h-6" />
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
            containerRef={containerRef}
            handleScroll={handleScroll}
            filtered={filtered}
            showAttrs={showAttrs}
            wrapLines={wrapLines}
            search={search}
            caseSensitive={caseSensitive}
          />

          {!following && (
            <button
              onClick={() => {
                setFollowing(true);
                if (containerRef.current) {
                  containerRef.current.scrollTo({ top: containerRef.current.scrollHeight, behavior: "smooth" });
                }
              }}
              className="absolute bottom-3 left-1/2 -translate-x-1/2 flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-full bg-card text-foreground border shadow-lg hover:bg-muted transition-colors"
            >
              <ArrowDown className="w-3 h-3" />
              Jump to bottom
            </button>
          )}
        </div>
      )}
    </div>
  );
}
