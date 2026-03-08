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
  Clock,
  ChevronDown,
} from "lucide-react";
import { api } from "../api/client";
import type { LogLine as ApiLogLine } from "../api/client";

interface Props {
  serviceId?: string;
  taskId?: string;
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

function detectLevel(msg: string): Level {
  const prefix = msg.slice(0, 120).toUpperCase();
  if (/\b(ERROR|ERRO|FATAL|PANIC|CRIT)\b/.test(prefix)) return "error";
  if (/\b(WARN|WARNING)\b/.test(prefix)) return "warn";
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

export default function LogViewer({ serviceId, taskId }: Props) {
  const logId = (serviceId || taskId)!;
  const isTask = !!taskId;
  const [lines, setLines] = useState<LogLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [limit, setLimit] = useState<number>(500);
  const [search, setSearch] = useState("");
  const [caseSensitive, setCaseSensitive] = useState(false);
  const [useRegex, setUseRegex] = useState(false);
  const [streamFilter, setStreamFilter] = useState<"all" | "stdout" | "stderr">("all");
  const [wrapLines, setWrapLines] = useState(true);
  const [following, setFollowing] = useState(true);
  const [live, setLive] = useState(false);
  const [timeRange, setTimeRange] = useState<TimeRange>({ label: "All" });
  const containerRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const streamParam = streamFilter === "all" ? undefined : streamFilter;

  const fetchLogs = useCallback(() => {
    setLoading(true);
    setError(null);
    const opts = { limit, after: timeRange.since, before: timeRange.until, stream: streamParam };
    const req = isTask ? api.taskLogs(logId, opts) : api.serviceLogs(logId, opts);
    req
      .then((resp) => {
        setLines((resp.lines ?? []).map(toLogLine));
        setLoading(false);
      })
      .catch(() => {
        setError("Failed to load logs");
        setLoading(false);
      });
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
        setLines((current) => [...current, toLogLine(parsed, current.length)]);
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
  }, [live, logId, isTask]); // eslint-disable-line react-hooks/exhaustive-deps

  // Auto-scroll to bottom when following
  useEffect(() => {
    if (following && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: "instant" as ScrollBehavior });
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

  return (
    <div className="flex flex-col gap-2">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-1.5">
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
        <div className="h-[400px] rounded-lg bg-gray-950 border border-gray-800" />
      ) : error ? (
        <div className="h-[400px] rounded-lg bg-gray-950 border border-gray-800 flex items-center justify-center">
          <div className="text-center">
            <p className="text-red-400 text-sm mb-3">{error}</p>
            <button
              onClick={fetchLogs}
              className="px-4 py-1.5 text-sm rounded-md bg-red-500/20 text-red-300 border border-red-500/30 hover:bg-red-500/30"
            >
              Retry
            </button>
          </div>
        </div>
      ) : filtered.length === 0 ? (
        <div className="h-[400px] rounded-lg bg-gray-950 border border-gray-800 flex items-center justify-center">
          <p className="text-gray-500 text-sm">
            {search ? "No matching log lines" : "No logs available"}
          </p>
        </div>
      ) : (
        <div className="relative">
          <div
            ref={containerRef}
            onScroll={handleScroll}
            className="h-[400px] overflow-auto rounded-lg bg-gray-950 border border-gray-800"
          >
            <table className="w-full border-collapse font-mono text-xs leading-5">
              <tbody>
                {filtered.map((line) => (
                  <tr key={line.index} className="hover:bg-white/[0.03] group">
                    <td className="w-[3px] p-0 align-stretch">
                      <div className={`w-[3px] min-h-full ${LEVEL_BAR[line.level]}`} />
                    </td>
                    <td className="pl-2 pr-1 py-px text-gray-600 text-right select-none align-top tabular-nums">
                      {line.index + 1}
                    </td>
                    <td
                      className="px-2 py-px text-gray-500 whitespace-nowrap align-top select-all"
                      title={line.timestamp}
                    >
                      {formatTime(line.timestamp)}
                    </td>
                    {showAttrs && (
                      <td
                        className="px-2 py-px text-gray-600 whitespace-nowrap align-top font-mono"
                        title={line.attrs?.taskId}
                      >
                        {line.attrs?.taskId?.slice(0, 8)}
                      </td>
                    )}
                    <td
                      className={`px-2 py-px text-gray-200 ${wrapLines ? "whitespace-pre-wrap break-all" : "whitespace-pre"}`}
                    >
                      <LogMessage line={line} search={search} caseSensitive={caseSensitive} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div ref={bottomRef} />
          </div>

          {!following && (
            <button
              onClick={() => {
                setFollowing(true);
                bottomRef.current?.scrollIntoView({ behavior: "smooth" });
              }}
              className="absolute bottom-3 left-1/2 -translate-x-1/2 flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-full bg-gray-800 text-gray-300 border border-gray-700 shadow-lg hover:bg-gray-700 transition-colors"
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

function LogMessage({
  line,
  search,
  caseSensitive,
}: {
  line: LogLine;
  search: string;
  caseSensitive: boolean;
}) {
  const msg = line.message;

  // If searching, highlight matches
  if (search) {
    return <HighlightedText text={msg} search={search} caseSensitive={caseSensitive} />;
  }

  // Auto-format JSON
  if (isJSON(msg)) {
    return <span className="text-emerald-300">{prettyJSON(msg)}</span>;
  }

  // Color error-level lines
  if (line.level === "error") return <span className="text-red-300">{msg}</span>;
  if (line.level === "warn") return <span className="text-yellow-300">{msg}</span>;
  if (line.level === "debug") return <span className="text-gray-400">{msg}</span>;

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
          <mark key={i} className="bg-yellow-500/40 text-yellow-200 rounded-[2px] px-[1px]">
            {part}
          </mark>
        ) : (
          <span key={i}>{part}</span>
        );
      })}
    </>
  );
}
