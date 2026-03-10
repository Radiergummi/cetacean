import type { LogLine as ApiLogLine } from "../api/client";

export interface LogLine extends ApiLogLine {
  index: number;
  level: Level;
}

export type Level = "error" | "warn" | "info" | "debug" | "default";

export interface TimeRange {
  since?: string;
  until?: string;
  label: string;
}

export const LIMIT_OPTIONS = [100, 500, 1000, 5000] as const;
export const MAX_LIVE_LINES = 10_000;
export const LOG_ROW_HEIGHT_ESTIMATE = 20;
export const LOG_VIRTUAL_THRESHOLD = 200;

export const PRESETS: { label: string; getValue: () => TimeRange }[] = [
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

export const RANGE_DURATIONS: Record<string, { label: string; ms: number }> = {
  "5m": { label: "Last 5m", ms: 5 * 60_000 },
  "15m": { label: "Last 15m", ms: 15 * 60_000 },
  "1h": { label: "Last 1h", ms: 60 * 60_000 },
  "6h": { label: "Last 6h", ms: 6 * 60 * 60_000 },
  "24h": { label: "Last 24h", ms: 24 * 60 * 60_000 },
  "7d": { label: "Last 7d", ms: 7 * 24 * 60 * 60_000 },
};

export const LABEL_TO_RANGE_KEY: Record<string, string> = Object.fromEntries(
  Object.entries(RANGE_DURATIONS).map(([k, v]) => [v.label, k]),
);

export const LEVEL_BAR: Record<Level, string> = {
  error: "bg-red-500",
  warn: "bg-yellow-500",
  info: "bg-blue-400",
  debug: "bg-gray-600",
  default: "bg-transparent",
};

export function classifyLevel(value: string): Level | null {
  const v = value.toUpperCase();
  if (v === "ERROR" || v === "ERRO" || v === "FATAL" || v === "PANIC" || v === "CRIT" || v === "CRITICAL") return "error";
  if (v === "WARN" || v === "WARNING") return "warn";
  if (v === "DEBUG" || v === "DEBG" || v === "TRACE") return "debug";
  if (v === "INFO") return "info";
  return null;
}

export const LEVEL_KEYS = ["level", "severity", "lvl", "loglevel", "log_level", "LEVEL"];

export function detectLevelFromJSON(msg: string): Level | null {
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

export function detectLevel(msg: string): Level {
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

export function toLogLine(api: ApiLogLine, index: number): LogLine {
  return { ...api, index, level: detectLevel(api.message) };
}

export function formatTime(ts: string): string {
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

export function isJSON(s: string): boolean {
  const t = s.trim();
  return (t.startsWith("{") && t.endsWith("}")) || (t.startsWith("[") && t.endsWith("]"));
}

export function prettyJSON(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

// Format an ISO string to datetime-local input value (YYYY-MM-DDTHH:mm)
export function toLocalInput(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function formatShortDate(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
