import InfoCard from "../InfoCard";

export default function ResourceId({
  label,
  id,
  truncate,
}: {
  label: string;
  id?: string;
  truncate?: number;
}) {
  if (!id) {
    return null;
  }

  const display = truncate ? id.slice(0, truncate) : id;
  const value =
    truncate && id.length > truncate ? (
      <span
        className="font-mono"
        title={id}
      >
        {display}
      </span>
    ) : (
      <span className="truncate font-mono">{display}</span>
    );

  return (
    <InfoCard
      label={label}
      value={value}
    />
  );
}
