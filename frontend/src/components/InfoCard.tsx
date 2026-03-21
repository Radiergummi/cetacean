import type React from "react";
import { Link } from "react-router-dom";

export default function InfoCard({
  label,
  value,
  href,
  className,
  right,
}: {
  label: string;
  value?: React.ReactNode | string;
  href?: string;
  className?: string;
  right?: React.ReactNode;
}) {
  const isExternal = href?.startsWith("http");
  const isString = typeof value === "string";

  return (
    <div
      data-right={right ? "" : undefined}
      className={`flex flex-col gap-y-1 rounded-lg border bg-card p-4 data-right:grid data-right:grid-cols-[1fr_auto] data-right:grid-rows-[auto_1fr] data-right:gap-x-3 ${className}`}
    >
      <span className="block text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {label}
      </span>

      <div
        className="inline-flex items-center gap-2 truncate text-base font-medium"
        title={isString ? value : undefined}
      >
        {value && href ? (
          isExternal ? (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-link hover:underline"
            >
              {value}
            </a>
          ) : (
            <Link
              to={href}
              className="text-link hover:underline"
            >
              {value}
            </Link>
          )
        ) : (
          value || "\u2014"
        )}
      </div>

      {right && <div className="col-start-2 row-span-2 row-start-1 flex items-center">{right}</div>}
    </div>
  );
}
