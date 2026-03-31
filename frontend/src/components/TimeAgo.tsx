import { formatDateTime, formatRelativeDate } from "../lib/format";
import { useEffect, useState } from "react";

export { formatRelativeDate as timeAgo };

export default function TimeAgo({ date }: { date: string }) {
  const [, setTick] = useState(0);

  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 60_000);
    return () => clearInterval(id);
  }, []);

  return (
    <time
      dateTime={date}
      title={formatDateTime(date)}
    >
      {formatRelativeDate(date)}
    </time>
  );
}
