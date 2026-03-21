/**
 *  Locale-aware number formatting with N decimal places.
 */
export function formatNumber(value: number, maximumFractionDigits = 0): string {
  return value.toLocaleString(undefined, { maximumFractionDigits });
}

/**
 * Format a value as a percentage using locale-aware formatting. Input is 0–100 range.
 */
export function formatPercentage(value: number, maximumFractionDigits = 1): string {
  value = value / 100;

  return value.toLocaleString(undefined, {
    style: "percent",
    maximumFractionDigits,
  });
}

/**
 * Format bytes as a human-readable size (e.g. "1.5 GB"). Uses 1024-based units.
 */
export function formatBytes(bytes: number): string {
  if (bytes >= 1024 ** 3) {
    return formatUnit(bytes / 1024 ** 3, "gigabyte", 1);
  }

  if (bytes >= 1024 ** 2) {
    return formatUnit(bytes / 1024 ** 2, "megabyte", 0);
  }

  if (bytes >= 1024) {
    return formatUnit(bytes / 1024, "kilobyte", 0);
  }

  return formatUnit(bytes, "byte", 0);
}

/**
 *  Format a CPU core count with locale-aware number (e.g. "1.50 cores").
 */
export function formatCores(cores: number, maximumFractionDigits = 2): string {
  return formatNumber(cores, maximumFractionDigits) + " cores";
}

/**
 * Format nanoseconds as a human-readable duration (e.g. "5s", "30min", "2h").
 */
export function formatDuration(nanoseconds: number, precise = false): string {
  if (nanoseconds <= 0) {
    return "—";
  }

  const ms = nanoseconds / 1_000_000;

  if (ms < 1000) {
    return formatUnit(Math.round(ms), "millisecond");
  }

  const sec = ms / 1000;

  if (sec < 60) {
    return formatUnit(Math.round(sec), "second");
  }

  const min = sec / 60;

  if (precise) {
    return new Intl.DurationFormat(undefined, { style: "narrow" }).format({
      hours: Math.floor(sec / 3600),
      minutes: Math.floor((sec % 3600) / 60),
      seconds: Math.round(sec % 60),
    });
  }

  if (min < 60) {
    return formatUnit(Math.round(min), "minute");
  }

  const hours = min / 60;

  if (hours < 24) {
    return formatUnit(Math.round(hours), "hour");
  }

  return formatUnit(Math.round(hours / 24), "day");
}

export function formatTimeRange(start: Date, end: Date): string {
  return new Intl.DateTimeFormat(undefined, {
    hour: "numeric",
    minute: "numeric",
    second: "numeric",
    fractionalSecondDigits: 2,
    timeStyle: "medium",
  }).formatRange(start, end);
}

/**
 * Format a metric value with appropriate units for chart display.
 */
export function formatMetricValue(value: number, unit?: string): string {
  if (unit === "bytes") {
    return formatBytes(value);
  }

  if (unit === "bytes/s") {
    return formatBytesPerSecond(value);
  }

  if (unit === "%") {
    return formatPercentage(value);
  }

  if (unit === "cores") {
    return formatNumber(value, 3);
  }

  return formatNumber(value, 2);
}

/**
 * Format bytes per second as a human-readable rate (e.g. "1.5 GB/s"). Uses 1024-based units.
 */
export function formatBytesPerSecond(bytes: number): string {
  if (bytes >= 1024 ** 3) {
    return formatUnit(bytes / 1024 ** 3, "gigabyte-per-second", 1);
  }

  if (bytes >= 1024 ** 2) {
    return formatUnit(bytes / 1024 ** 2, "megabyte-per-second", 0);
  }

  if (bytes >= 1024) {
    return formatUnit(bytes / 1024, "kilobyte-per-second", 0);
  }

  return formatUnit(bytes, "byte-per-second", 0);
}

/**
 * Locale-aware date string (e.g. "3/18/2026" or "18.03.2026").
 */
export function formatDate(date: string | Date, options?: Intl.DateTimeFormatOptions): string {
  return new Date(date).toLocaleDateString(undefined, options);
}

/**
 * Locale-aware time string (e.g. "2:30:00 PM" or "14:30:00").
 */
export function formatTime(date: string | Date, options?: Intl.DateTimeFormatOptions): string {
  return new Date(date).toLocaleTimeString(undefined, options);
}

/**
 * Locale-aware date and time string.
 */
export function formatDateTime(date: string | Date, options?: Intl.DateTimeFormatOptions): string {
  return new Date(date).toLocaleString(undefined, options);
}

const relativeThresholds = [
  [60, "just now", 0],
  [3600, "m ago", 60],
  [86400, "h ago", 3600],
  [2592000, "d ago", 86400],
] as const;

/**
 * Format a date as a relative time string (e.g. "5m ago", "2h ago"). Falls back to locale date for dates older than 30 days.
 */
export function formatRelativeDate(date: string): string {
  const seconds = Math.floor((Date.now() - new Date(date).getTime()) / 1000);

  if (seconds < 0) {
    return "just now";
  }

  for (const [max, suffix, divisor] of relativeThresholds) {
    if (seconds < max) {
      return divisor === 0 ? suffix : `${Math.floor(seconds / divisor)}${suffix}`;
    }
  }

  return formatDate(date);
}

/**
 * Convert nanoseconds to seconds. Returns undefined if the input is nullish or zero.
 */
export function nanosToSeconds(nanoseconds: number | undefined): number | undefined {
  if (!nanoseconds) {
    return undefined;
  }

  return nanoseconds / 1e9;
}

/**
 * Format a number with a specified unit using `Intl.NumberFormat`.
 */
function formatUnit(value: number, unit: string, maximumFractionDigits = 0): string {
  return new Intl.NumberFormat(undefined, {
    style: "unit",
    unit,
    unitDisplay: "narrow",
    maximumFractionDigits,
  }).format(value);
}
