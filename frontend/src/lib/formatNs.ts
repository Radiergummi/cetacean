/** Format nanoseconds as a human-readable duration (e.g. "5s", "30m", "2h", "7d"). */
export function formatNs(ns: number): string {
  if (ns <= 0) return "—";

  const ms = ns / 1_000_000;
  if (ms < 1000) return `${Math.round(ms)}ms`;

  const sec = ms / 1000;
  if (sec < 60) return `${Math.round(sec)}s`;

  const min = sec / 60;
  if (min < 60) return `${Math.round(min)}m`;

  const hrs = min / 60;
  if (hrs < 24) return `${Math.round(hrs)}h`;

  return `${Math.round(hrs / 24)}d`;
}
