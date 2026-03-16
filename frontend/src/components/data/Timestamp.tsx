import InfoCard from "../InfoCard";
import TimeAgo from "../TimeAgo";

export default function Timestamp({
  label,
  date,
  relative = true,
}: {
  label: string;
  date?: string;
  relative?: boolean;
}) {
  if (!date) return null;

  const value = relative ? <TimeAgo date={date} /> : new Date(date).toLocaleString();

  return (
    <InfoCard
      label={label}
      value={value}
    />
  );
}
