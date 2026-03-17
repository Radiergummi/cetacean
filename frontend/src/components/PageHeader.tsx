import { ChevronRight } from "lucide-react";
import type React from "react";
import { useEffect } from "react";
import { Link } from "react-router-dom";

interface Crumb {
  label: React.ReactNode;
  to?: string;
}

interface Props {
  title: React.ReactNode;
  subtitle?: string;
  breadcrumbs?: Crumb[];
  actions?: React.ReactNode;
}

export default function PageHeader({ title, subtitle, breadcrumbs, actions }: Props) {
  useEffect(() => {
    const text = typeof title === "string" ? title : null;

    if (text) {
      document.title = `${text} · Cetacean`;
    }

    return () => {
      document.title = "Cetacean";
    };
  }, [title]);

  return (
    <header className="mb-6 flex max-w-full flex-col gap-6 overflow-x-hidden">
      {breadcrumbs && breadcrumbs.length > 0 && (
        <nav className="mb-1.5 flex items-center gap-1 text-sm text-muted-foreground">
          {breadcrumbs.map(({ label, to }, index) => (
            <span
              key={index}
              className="flex items-center gap-1"
            >
              {index > 0 && <ChevronRight className="size-3.5" />}
              {to ? (
                <Link
                  to={to}
                  className="transition-colors hover:text-foreground"
                >
                  {label}
                </Link>
              ) : (
                <span className="truncate text-foreground">{label}</span>
              )}
            </span>
          ))}
        </nav>
      )}

      <div className="flex items-center justify-between gap-4 truncate">
        <div>
          <h1 className="max-w-full text-2xl font-semibold tracking-tight slashed-zero tabular-nums">
            {title}
          </h1>
          {subtitle && <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>}
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </div>
    </header>
  );
}
