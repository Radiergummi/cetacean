import { Link } from "react-router-dom";

export default function ResourceCard({
  title,
  to,
  badge,
  subtitle,
  children,
  meta,
}: {
  title: string;
  to?: string;
  badge?: React.ReactNode;
  subtitle?: React.ReactNode;
  children?: React.ReactNode;
  meta?: React.ReactNode[];
}) {
  const hasBody = subtitle || children || meta;
  const content = (
    <>
      <div className={`flex items-center justify-between ${hasBody ? "mb-3" : ""}`}>
        <span className="font-medium truncate">{title}</span>
        {badge}
      </div>
      {subtitle && (
        <div className="text-xs font-mono text-muted-foreground truncate mb-3">{subtitle}</div>
      )}
      {children && <div className="mb-3">{children}</div>}
      {meta && meta.length > 0 && (
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          {meta.map((item, i) => (
            <span key={i}>{item}</span>
          ))}
        </div>
      )}
    </>
  );

  const className =
    "rounded-lg border bg-card p-4" +
    (to ? " hover:border-foreground/20 hover:shadow-sm transition-all" : "");

  if (to) {
    return (
      <Link to={to} className={className}>
        {content}
      </Link>
    );
  }

  return <div className={className}>{content}</div>;
}
