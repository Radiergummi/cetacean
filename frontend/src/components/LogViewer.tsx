import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
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
import type { LogLine, TimeRange, Level } from "./log-utils";
import { LIMIT_OPTIONS, MAX_LIVE_LINES, RANGE_DURATIONS, LABEL_TO_RANGE_KEY, formatShortDate, toLogLine } from "./log-utils";
import { LogTable } from "./LogTable";
import { TimeRangeSelector, StreamFilterToggle, LevelFilter, ToolbarButton } from "./LogToolbar";

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
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [hasOlderLogs, setHasOlderLogs] = useState(true);
  const [loadingNewer, setLoadingNewer] = useState(false);
  const oldestRef = useRef<string | undefined>(undefined);
  const newestRef = useRef<string | undefined>(undefined);
  const [limit, setLimit] = useState<number>(500);
  const [search, setSearch] = useState("");
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [matchIndex, setMatchIndex] = useState(0);
  const [useRegex, setUseRegex] = useState(false);
  const [streamFilter, setStreamFilter] = useState<"all" | "stdout" | "stderr">("all");
  const [levelFilter, setLevelFilter] = useState<Level | "all">("all");
  const [wrapLines, setWrapLines] = useState(false);
  const [following, setFollowing] = useState(true);
  const [live, setLive] = useState(false);
  const [params, setParams] = useSearchParams();

  const initialTimeRange = useMemo((): TimeRange => {
    const rangeKey = params.get("logRange");
    if (rangeKey && RANGE_DURATIONS[rangeKey]) {
      const { label, ms } = RANGE_DURATIONS[rangeKey];
      return { since: new Date(Date.now() - ms).toISOString(), label };
    }
    const logSince = params.get("logSince");
    const logUntil = params.get("logUntil");
    if (logSince || logUntil) {
      let label = "Custom";
      if (logSince && logUntil) label = `${formatShortDate(logSince)} – ${formatShortDate(logUntil)}`;
      else if (logSince) label = `Since ${formatShortDate(logSince)}`;
      else if (logUntil) label = `Until ${formatShortDate(logUntil)}`;
      return { since: logSince || undefined, until: logUntil || undefined, label };
    }
    return { label: "All" };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [timeRange, setTimeRange] = useState<TimeRange>(initialTimeRange);

  const updateTimeRange = useCallback((tr: TimeRange) => {
    setTimeRange(tr);
    setParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete("logRange");
      next.delete("logSince");
      next.delete("logUntil");

      const rangeKey = LABEL_TO_RANGE_KEY[tr.label];
      if (rangeKey) {
        next.set("logRange", rangeKey);
      } else if (tr.since || tr.until) {
        if (tr.since) next.set("logSince", tr.since);
        if (tr.until) next.set("logUntil", tr.until);
      }
      return next;
    }, { replace: true });
  }, [setParams]);
  const containerRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const streamParam = streamFilter === "all" ? undefined : streamFilter;

  const fetchLogs = useCallback(() => {
    abortRef.current?.abort();
    setLoading(true);
    setError(null);
    setHasOlderLogs(true);
    oldestRef.current = undefined;
    newestRef.current = undefined;
    const controller = new AbortController();
    abortRef.current = controller;
    let timedOut = false;
    const timeout = setTimeout(() => { timedOut = true; controller.abort(); }, 15_000);
    const opts = { limit, after: timeRange.since, before: timeRange.until, stream: streamParam, signal: controller.signal };
    const req = isTask ? api.taskLogs(logId, opts) : api.serviceLogs(logId, opts);
    req
      .then((resp) => {
        const newLines = (resp.lines ?? []).map(toLogLine);
        setLines(newLines);
        oldestRef.current = resp.oldest;
        newestRef.current = resp.newest;
        setHasOlderLogs(newLines.length >= limit);
        setLoading(false);
      })
      .catch((err) => {
        if (timedOut) {
          setError("Request timed out");
        } else if (!controller.signal.aborted) {
          setError(err instanceof Error ? err.message : "Failed to load logs");
        }
        setLoading(false);
      })
      .finally(() => clearTimeout(timeout));
  }, [logId, isTask, limit, timeRange, streamParam]);

  useEffect(() => {
    fetchLogs();
    return () => {
      abortRef.current?.abort();
    };
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
    const buffer: ApiLogLine[] = [];
    let rafId = 0;

    const flush = () => {
      rafId = 0;
      if (buffer.length === 0) return;
      const batch = buffer.splice(0);
      setLines((current) => {
        const next = current.concat(batch.map((l, i) => toLogLine(l, current.length + i)));
        return next.length > MAX_LIVE_LINES ? next.slice(-MAX_LIVE_LINES) : next;
      });
    };

    es.onmessage = (event) => {
      try {
        buffer.push(JSON.parse(event.data));
        if (!rafId) rafId = requestAnimationFrame(flush);
      } catch {
        // skip malformed events
      }
    };

    es.onerror = () => {};

    return () => {
      es.close();
      cancelAnimationFrame(rafId);
      abortRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live, logId, isTask, streamParam]);

  // Auto-scroll to bottom when following (scroll within the container, not the page)
  useEffect(() => {
    if (following && containerRef.current) {
      const el = containerRef.current;
      el.scrollTop = el.scrollHeight;
    }
  }, [lines, following]);

  const loadOlder = useCallback(() => {
    if (loadingOlder || !hasOlderLogs || !oldestRef.current) return;
    setLoadingOlder(true);
    const opts = { limit, before: oldestRef.current, stream: streamParam };
    const req = isTask ? api.taskLogs(logId, opts) : api.serviceLogs(logId, opts);
    req.then((resp) => {
      const older = (resp.lines ?? []).map(toLogLine);
      if (older.length === 0) {
        setHasOlderLogs(false);
      } else {
        oldestRef.current = resp.oldest;
        const scrollEl = containerRef.current;
        const prevScrollHeight = scrollEl?.scrollHeight ?? 0;
        setLines((current) => {
          const combined = [...older, ...current];
          return combined.map((l, i) => ({ ...l, index: i }));
        });
        requestAnimationFrame(() => {
          if (scrollEl) {
            scrollEl.scrollTop += scrollEl.scrollHeight - prevScrollHeight;
          }
        });
      }
      setLoadingOlder(false);
    }).catch(() => setLoadingOlder(false));
  }, [loadingOlder, hasOlderLogs, limit, streamParam, isTask, logId]);

  const loadNewer = useCallback(() => {
    if (loadingNewer || !newestRef.current) return;
    setLoadingNewer(true);
    const opts = { limit, after: newestRef.current, stream: streamParam };
    const req = isTask ? api.taskLogs(logId, opts) : api.serviceLogs(logId, opts);
    req.then((resp) => {
      const newer = (resp.lines ?? []).map(toLogLine);
      if (newer.length > 0) {
        newestRef.current = resp.newest;
        setLines((current) => {
          const combined = [...current, ...newer];
          return combined.map((l, i) => ({ ...l, index: i }));
        });
      }
      setLoadingNewer(false);
    }).catch(() => setLoadingNewer(false));
  }, [loadingNewer, limit, streamParam, isTask, logId]);

  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
    setFollowing(atBottom);
    if (el.scrollTop < 100) {
      loadOlder();
    }
    if (atBottom && !live) {
      loadNewer();
    }
  }, [loadOlder, loadNewer, live]);

  const showAttrs = !isTask && lines.some((l) => l.attrs?.taskId);

  const filtered = useMemo(() => {
    let result = lines;
    if (streamFilter !== "all") {
      result = result.filter((l) => l.stream === streamFilter);
    }
    if (levelFilter !== "all") {
      result = result.filter((l) => l.level === levelFilter);
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
  }, [lines, search, caseSensitive, useRegex, streamFilter, levelFilter]);

  useEffect(() => {
    setMatchIndex(0);
  }, [filtered]);

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
            updateTimeRange(tr);
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
        <LevelFilter value={levelFilter} onChange={setLevelFilter} />

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
              if (e.key === "Escape") { setSearch(""); return; }
              if (e.key === "Enter" && search && filtered.length > 0) {
                e.preventDefault();
                if (e.shiftKey) {
                  setMatchIndex((i) => (i - 1 + filtered.length) % filtered.length);
                } else {
                  setMatchIndex((i) => (i + 1) % filtered.length);
                }
              }
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
            {filtered.length > 0 ? `${matchIndex + 1}/${filtered.length}` : "0/0"}
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
            highlightIndex={search && filtered.length > 0 ? filtered[matchIndex]?.index : undefined}
            scrollToFiltered={search && filtered.length > 0 ? matchIndex : undefined}
            loadingOlder={loadingOlder}
            hasOlderLogs={hasOlderLogs}
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
