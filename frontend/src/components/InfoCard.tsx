import { Link } from "react-router-dom";

export default function InfoCard({
  label,
  value,
  href,
}: {
  label: string;
  value?: string;
  href?: string;
}) {
  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1">
        {label}
      </div>
      <div className="text-base font-medium truncate" title={value || undefined}>
        {value && href ? (
          <Link to={href} className="text-link hover:underline">
            {value}
          </Link>
        ) : (
          value || "\u2014"
        )}
      </div>
    </div>
  );
}
