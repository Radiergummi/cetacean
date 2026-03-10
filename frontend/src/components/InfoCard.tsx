import type React from "react";
import {Link} from "react-router-dom";

export default function InfoCard({
    label,
    value,
    href,
    className,
}: {
    label: string;
    value?: React.ReactNode | string;
    href?: string;
    className?: string;
}) {
    const isExternal = href?.startsWith("http");
    const isString = typeof value === "string";

    return (
        <div className={`rounded-lg border bg-card p-4 ${className}`}>
            <span className="block text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1">
                {label}
            </span>

            <div
                className="inline-flex items-center gap-2 text-base font-medium truncate"
                title={isString ? value : undefined}
            >
                {value && href && isString ? (
                    isExternal ? (
                        <a href={href} target="_blank" rel="noopener noreferrer" className="text-link hover:underline">
                            {value}
                        </a>
                    ) : (
                        <Link to={href} className="text-link hover:underline">
                            {value}
                        </Link>
                    )
                ) : (
                    value || "\u2014"
                )}
            </div>
        </div>
    );
}
