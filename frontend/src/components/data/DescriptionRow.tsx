export function DescriptionRow({
  label,
  value,
  mono,
}: {
  label: string;
  value: string | undefined;
  mono?: boolean;
}) {
  return (
    <div className="grid grid-cols-[8rem_1fr] items-baseline gap-x-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={mono ? "font-mono" : undefined}>{value || "—"}</dd>
    </div>
  );
}
