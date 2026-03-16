const THRESHOLDS: [number, string, number][] = [
  [60, "just now", 0],
  [3600, "m ago", 60],
  [86400, "h ago", 3600],
  [2592000, "d ago", 86400],
];

export function timeAgo(date: string): string {
  const seconds = Math.floor((Date.now() - new Date(date).getTime()) / 1000);
  if (seconds < 0) return "just now";
  for (const [max, suffix, divisor] of THRESHOLDS) {
    if (seconds < max) return divisor === 0 ? suffix : `${Math.floor(seconds / divisor)}${suffix}`;
  }
  return new Date(date).toLocaleDateString();
}

export default function TimeAgo({ date }: { date: string }) {
  const full = new Date(date).toLocaleString();
  return (
    <time
      dateTime={date}
      title={full}
    >
      {timeAgo(date)}
    </time>
  );
}
