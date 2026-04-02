import { feedForPath } from "@/components/AtomFeedLink";
import { buttonVariants } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ChevronRight, Rss } from "lucide-react";
import type React from "react";
import { useEffect } from "react";
import { Link, useLocation } from "react-router-dom";

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
  const { pathname, search } = useLocation();
  const feed = feedForPath(pathname, search);

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
        <nav
          aria-label="Breadcrumb"
          className="mb-1.5 text-sm text-muted-foreground"
        >
          <ol className="flex items-center gap-1">
            {breadcrumbs.map(({ label, to }, index) => (
              <li
                key={index}
                className="flex items-center gap-1"
              >
                {index > 0 && (
                  <ChevronRight
                    aria-hidden="true"
                    className="size-3.5"
                  />
                )}
                {to ? (
                  <Link
                    to={to}
                    className="transition-colors hover:text-foreground"
                  >
                    {label}
                  </Link>
                ) : (
                  <span
                    aria-current="page"
                    className="truncate text-foreground"
                  >
                    {label}
                  </span>
                )}
              </li>
            ))}
          </ol>
        </nav>
      )}

      <div className="flex items-center justify-between gap-4 truncate">
        <div>
          <h1 className="max-w-full text-2xl font-semibold tracking-tight slashed-zero tabular-nums">
            {title}
          </h1>
          {subtitle && <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>}
        </div>
        {(actions || feed) && (
          <div className="flex items-center gap-2">
            {actions}
            {feed && (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <a
                      href={feed.href + ".atom"}
                      className={buttonVariants({ variant: "ghost", size: "icon-sm" })}
                      aria-label="Atom feed"
                    >
                      <Rss className="size-3.5" />
                    </a>
                  }
                />
                <TooltipContent>Atom feed</TooltipContent>
              </Tooltip>
            )}
          </div>
        )}
      </div>
    </header>
  );
}
