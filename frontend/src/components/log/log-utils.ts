import type { LogLine as ApiLogLine } from "../../api/client";

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

export const maxLiveLines = 10_000;
export const logRowHeightEstimate = 20;
export const logVirtualThreshold = 200;

export const presets: { label: string; getValue: () => TimeRange }[] = [
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

export const rangeDurations: Record<string, { label: string; ms: number }> = {
  "5m": { label: "Last 5m", ms: 5 * 60_000 },
  "15m": { label: "Last 15m", ms: 15 * 60_000 },
  "1h": { label: "Last 1h", ms: 60 * 60_000 },
  "6h": { label: "Last 6h", ms: 6 * 60 * 60_000 },
  "24h": { label: "Last 24h", ms: 24 * 60 * 60_000 },
  "7d": { label: "Last 7d", ms: 7 * 24 * 60 * 60_000 },
};

export const labelToRangeKey: Record<string, string> = Object.fromEntries(
  Object.entries(rangeDurations).map(([key, value]) => [value.label, key]),
);

export const levelBar: Record<Level, string> = {
  error: "bg-red-400",
  warn: "bg-yellow-500",
  info: "bg-blue-300",
  debug: "bg-gray-600",
  default: "bg-transparent",
};

export function classifyLevel(value: string): Level | null {
  const v = value.toUpperCase();

  if (
    v === "ERROR" ||
    v === "ERRO" ||
    v === "FATAL" ||
    v === "PANIC" ||
    v === "CRIT" ||
    v === "CRITICAL"
  ) {
    return "error";
  }

  if (v === "WARN" || v === "WARNING") {
    return "warn";
  }

  if (v === "DEBUG" || v === "DEBG" || v === "TRACE") {
    return "debug";
  }

  if (v === "INFO") {
    return "info";
  }

  return null;
}

export const levelKeys = ["level", "severity", "lvl", "loglevel", "log_level", "LEVEL"];

export function detectLevelFromJSON(message: string): Level | null {
  try {
    const parsedData = JSON.parse(message);

    if (typeof parsedData !== "object" || parsedData === null) {
      return null;
    }

    for (const key of levelKeys) {
      const val = parsedData[key];

      if (typeof val === "string") {
        const level = classifyLevel(val);

        if (level) {
          return level;
        }
      }
    }
    // Also check numeric slog-style levels (slog: DEBUG=-4, INFO=0, WARN=4, ERROR=8)
    const numericValue = parsedData.level ?? parsedData.severity;

    if (typeof numericValue === "number") {
      if (numericValue >= 8) {
        return "error";
      }

      if (numericValue >= 4) {
        return "warn";
      }

      if (numericValue < 0) {
        return "debug";
      }

      return "info";
    }
  } catch {
    // not JSON
  }

  return null;
}

export function detectLevel(message: string): Level {
  // Try structured JSON first
  if (message.length > 0 && message[0] === "{") {
    const level = detectLevelFromJSON(message);

    if (level) {
      return level;
    }
  }

  // Fall back to regex on prefix
  const prefix = message.slice(0, 200).toUpperCase();

  if (/\b(ERROR|ERRO|FATAL|PANIC|CRIT(ICAL)?)\b/.test(prefix)) {
    return "error";
  }

  if (/\b(WARN(ING)?)\b/.test(prefix)) {
    return "warn";
  }

  if (/\b(DEBUG|DEBG|TRACE)\b/.test(prefix)) {
    return "debug";
  }

  if (/\bINFO\b/.test(prefix)) {
    return "info";
  }

  return "default";
}

export function toLogLine(api: ApiLogLine, index: number): LogLine {
  return { ...api, index, level: detectLevel(api.message) };
}

export function formatTime(timestamp: string): string {
  if (!timestamp) {
    return "";
  }

  try {
    return new Date(timestamp).toLocaleTimeString(undefined, {
      hour12: false,
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return timestamp;
  }
}

export function isJSON(string: string): boolean {
  const trimmed = string.trim();

  return (
    (trimmed.startsWith("{") && trimmed.endsWith("}")) ||
    (trimmed.startsWith("[") && trimmed.endsWith("]"))
  );
}

export function prettyJSON(string: string): string {
  try {
    return JSON.stringify(JSON.parse(string), null, 2);
  } catch {
    return string;
  }
}

// Format an ISO string to datetime-local input value (YYYY-MM-DDTHH:mm)
export function toLocalInput(isoString: string): string {
  const date = new Date(isoString);
  const pad = (number: number) => String(number).padStart(2, "0");

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function logLineKey({ attrs, message, timestamp }: LogLine): string {
  return `${timestamp}\x00${attrs?.taskId ?? ""}\x00${message}`;
}

export function formatShortDate(isoString: string): string {
  const date = new Date(isoString);
  const pad = (number: number) => String(number).padStart(2, "0");

  return `${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
