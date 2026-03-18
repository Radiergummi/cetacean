import { formatDateTime, formatRelativeDate } from "../lib/format";

export { formatRelativeDate as timeAgo };

export default function TimeAgo({ date }: { date: string }) {
  return (
    <time
      dateTime={date}
      title={formatDateTime(date)}
    >
      {formatRelativeDate(date)}
    </time>
  );
}
