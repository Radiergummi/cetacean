import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
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
  Clock,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  Loader2,
  FileText,
} from "lucide-react";
import { api } from "../api/client";
import type { LogLine as ApiLogLine } from "../api/client";

interface Props {
  serviceId?: string;
  taskId?: string;
  header?: React.ReactNode;
}

interface LogLine extends ApiLogLine {
  index: number;
  level: Level;
}

type Level = "error" | "warn" | "info" | "debug" | "default";

interface TimeRange {
  since?: string;
  until?: string;
  label: string;
}

const LIMIT_OPTIONS = [100, 500, 1000, 5000] as const;
const MAX_LIVE_LINES = 10_000;
const LOG_ROW_HEIGHT_ESTIMATE = 20;
const LOG_VIRTUAL_THRESHOLD = 200;

const PRESETS: { label: string; getValue: () => TimeRange }[] = [
  { label: "All", getValue: () => ({ label: "All" }) },
  {
    label: "Last 5m",
    getValue: () => ({
      since: new Date(Date.now() - 5 * 60_000).toISOString(),
      label: "Last 5m",
    }),
  },
  {
    label: "Last 15m",
    getValue: () => ({
      since: new Date(Date.now() - 15 * 60_000).toISOString(),
      label: "Last 15m",
    }),
  },
  {
    label: "Last 1h",
    getValue: () => ({
      since: new Date(Date.now() - 60 * 60_000).toISOString(),
      label: "Last 1h",
    }),
  },
  {
    label: "Last 6h",
    getValue: () => ({
      since: new Date(Date.now() - 6 * 60 * 60_000).toISOString(),
      label: "Last 6h",
    }),
  },
  {
    label: "Last 24h",
    getValue: () => ({
      since: new Date(Date.now() - 24 * 60 * 60_000).toISOString(),
      label: "Last 24h",
    }),
  },
  {
    label: "Last 7d",
    getValue: () => ({
      since: new Date(Date.now() - 7 * 24 * 60 * 60_000).toISOString(),
      label: "Last 7d",
    }),
  },
];

const LEVEL_BAR: Record<Level, string> = {
  error: "bg-red-500",
  warn: "bg-yellow-500",
  info: "bg-blue-400",
  debug: "bg-gray-600",
  default: "bg-transparent",
};

function classifyLevel(value: string): Level | null {
  const v = value.toUpperCase();
  if (v === "ERROR" || v === "ERRO" || v === "FATAL" || v === "PANIC" || v === "CRIT" || v === "CRITICAL") return "error";
  if (v === "WARN" || v === "WARNING") return "warn";
  if (v === "DEBUG" || v === "DEBG" || v === "TRACE") return "debug";
  if (v === "INFO") return "info";
  return null;
}

const LEVEL_KEYS = ["level", "severity", "lvl", "loglevel", "log_level", "LEVEL"];

function detectLevelFromJSON(msg: string): Level | null {
  try {
    const obj = JSON.parse(msg);
    if (typeof obj !== "object" || obj === null) return null;
    for (const key of LEVEL_KEYS) {
      const val = obj[key];
      if (typeof val === "string") {
        const level = classifyLevel(val);
        if (level) return level;
      }
    }
    // Also check numeric slog-style levels (slog: DEBUG=-4, INFO=0, WARN=4, ERROR=8)
    const numVal = obj.level ?? obj.severity;
    if (typeof numVal === "number") {
      if (numVal >= 8) return "error";
      if (numVal >= 4) return "warn";
      if (numVal < 0) return "debug";
      return "info";
    }
  } catch {
    // not JSON
  }
  return null;
}

function detectLevel(msg: string): Level {
  // Try structured JSON first
  if (msg.length > 0 && msg[0] === "{") {
    const level = detectLevelFromJSON(msg);
    if (level) return level;
  }

  // Fall back to regex on prefix
  const prefix = msg.slice(0, 200).toUpperCase();
  if (/\b(ERROR|ERRO|FATAL|PANIC|CRIT(ICAL)?)\b/.test(prefix)) return "error";
  if (/\b(WARN(ING)?)\b/.test(prefix)) return "warn";
  if (/\b(DEBUG|DEBG|TRACE)\b/.test(prefix)) return "debug";
  if (/\bINFO\b/.test(prefix)) return "info";
  return "default";
}

function toLogLine(api: ApiLogLine, index: number): LogLine {
  return { ...api, index, level: detectLevel(api.message) };
}

function formatTime(ts: string): string {
  if (!ts) return "";
  try {
    return new Date(ts).toLocaleTimeString(undefined, {
      hour12: false,
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return ts;
  }
}

function isJSON(s: string): boolean {
  const t = s.trim();
  return (t.startsWith("{") && t.endsWith("}")) || (t.startsWith("[") && t.endsWith("]"));
}

function prettyJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

// Format an ISO string to datetime-local input value (YYYY-MM-DDTHH:mm)
function toLocalInput(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
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

function TimeRangeSelector({
  value,
  onChange,
}: {
  value: TimeRange;
  onChange: (tr: TimeRange) => void;
}) {
  const [open, setOpen] = useState(false);
  const [customSince, setCustomSince] = useState("");
  const [customUntil, setCustomUntil] = useState("");
  const ref = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  // Sync custom inputs when opening
  useEffect(() => {
    if (open) {
      setCustomSince(value.since ? toLocalInput(value.since) : "");
      setCustomUntil(value.until ? toLocalInput(value.until) : "");
    }
  }, [open]);

  const applyCustom = () => {
    const since = customSince ? new Date(customSince).toISOString() : undefined;
    const until = customUntil ? new Date(customUntil).toISOString() : undefined;

    let label = "Custom";
    if (since && until) {
      label = `${formatShortDate(since)} \u2013 ${formatShortDate(until)}`;
    } else if (since) {
      label = `Since ${formatShortDate(since)}`;
    } else if (until) {
      label = `Until ${formatShortDate(until)}`;
    }

    onChange({ since, until, label });
    setOpen(false);
  };

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen(!open)}
        className={`h-8 inline-flex items-center gap-1.5 px-2.5 text-xs border rounded-md ${
          value.since || value.until
            ? "bg-primary/10 border-primary/30 text-primary"
            : "bg-background hover:bg-muted"
        }`}
        title="Time range"
      >
        <Clock className="w-3.5 h-3.5" />
        <span className="max-w-32 truncate">{value.label}</span>
        <ChevronDown className="w-3 h-3 opacity-50" />
      </button>

      {open && (
        <div className="absolute top-full left-0 mt-1 z-50 w-72 rounded-lg border bg-popover shadow-lg">
          {/* Presets */}
          <div className="p-2 border-b">
            <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground mb-1.5 px-1">
              Presets
            </div>
            <div className="flex flex-wrap gap-1">
              {PRESETS.map((p) => (
                <button
                  key={p.label}
                  onClick={() => {
                    onChange(p.getValue());
                    setOpen(false);
                  }}
                  className={`px-2 py-1 text-xs rounded-md ${
                    value.label === p.label
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted hover:bg-muted/80 text-foreground"
                  }`}
                >
                  {p.label}
                </button>
              ))}
            </div>
          </div>

          {/* Custom range */}
          <div className="p-2">
            <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground mb-1.5 px-1">
              Custom Range
            </div>
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-xs">
                <span className="w-10 text-muted-foreground">From</span>
                <input
                  type="datetime-local"
                  value={customSince}
                  onChange={(e) => setCustomSince(e.target.value)}
                  className="flex-1 h-7 px-2 text-xs border rounded-md bg-background"
                />
                {customSince && (
                  <button
                    onClick={() => setCustomSince("")}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="w-3 h-3" />
                  </button>
                )}
              </label>
              <label className="flex items-center gap-2 text-xs">
                <span className="w-10 text-muted-foreground">To</span>
                <input
                  type="datetime-local"
                  value={customUntil}
                  onChange={(e) => setCustomUntil(e.target.value)}
                  className="flex-1 h-7 px-2 text-xs border rounded-md bg-background"
                />
                {customUntil && (
                  <button
                    onClick={() => setCustomUntil("")}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="w-3 h-3" />
                  </button>
                )}
              </label>
              <button
                onClick={applyCustom}
                disabled={!customSince && !customUntil}
                className="w-full h-7 text-xs font-medium rounded-md bg-primary text-primary-foreground disabled:opacity-40"
              >
                Apply
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function formatShortDate(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const STREAM_OPTIONS = ["all", "stdout", "stderr"] as const;

function StreamFilterToggle({
  value,
  onChange,
}: {
  value: "all" | "stdout" | "stderr";
  onChange: (v: "all" | "stdout" | "stderr") => void;
}) {
  return (
    <div className="flex items-center h-8 rounded-md border bg-background overflow-hidden">
      {STREAM_OPTIONS.map((opt) => (
        <button
          key={opt}
          onClick={() => onChange(opt)}
          className={`px-2 h-full text-xs ${
            value === opt
              ? "bg-primary text-primary-foreground"
              : "text-muted-foreground hover:text-foreground hover:bg-muted"
          }`}
          title={opt === "all" ? "All streams" : opt}
        >
          {opt === "all" ? "All" : opt}
        </button>
      ))}
    </div>
  );
}

function ToolbarButton({
  onClick,
  title,
  icon,
  active,
}: {
  onClick: () => void;
  title: string;
  icon: React.ReactNode;
  active?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      title={title}
      className={`h-8 w-8 flex items-center justify-center rounded-md border ${
        active
          ? "bg-primary text-primary-foreground border-primary"
          : "bg-background hover:bg-muted border-border"
      }`}
    >
      {icon}
    </button>
  );
}

interface LogTableProps {
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

function LogTable({ containerRef, handleScroll, filtered, showAttrs, wrapLines, search, caseSensitive }: LogTableProps) {
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

function LogMessage({
  line,
  search,
  caseSensitive,
  prettyJson,
}: {
  line: LogLine;
  search: string;
  caseSensitive: boolean;
  prettyJson: boolean;
}) {
  const msg = line.message;

  // If searching, highlight matches
  if (search) {
    const text = prettyJson && isJSON(msg) ? prettyJSON(msg) : msg;
    return <HighlightedText text={text} search={search} caseSensitive={caseSensitive} />;
  }

  // Auto-format JSON when pretty-printing is enabled
  if (isJSON(msg)) {
    const text = prettyJson ? prettyJSON(msg) : msg;
    return <span className="text-emerald-700 dark:text-emerald-300">{text}</span>;
  }

  // Color error-level lines
  if (line.level === "error") return <span className="text-red-600 dark:text-red-300">{msg}</span>;
  if (line.level === "warn") return <span className="text-yellow-700 dark:text-yellow-300">{msg}</span>;
  if (line.level === "debug") return <span className="text-muted-foreground">{msg}</span>;

  return <>{msg}</>;
}

function HighlightedText({
  text,
  search,
  caseSensitive,
}: {
  text: string;
  search: string;
  caseSensitive: boolean;
}) {
  const escaped = search.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const parts = text.split(new RegExp(`(${escaped})`, caseSensitive ? "g" : "gi"));

  return (
    <>
      {parts.map((part, i) => {
        const isMatch = caseSensitive
          ? part === search
          : part.toLowerCase() === search.toLowerCase();
        return isMatch ? (
          <mark key={i} className="bg-yellow-200 dark:bg-yellow-500/40 text-yellow-900 dark:text-yellow-200 rounded-[2px] px-[1px]">
            {part}
          </mark>
        ) : (
          <span key={i}>{part}</span>
        );
      })}
    </>
  );
}
