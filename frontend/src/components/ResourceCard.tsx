import type React from "react";
import {Link} from "react-router-dom";

export default function ResourceCard({
    title,
    to,
    badge,
    subtitle,
    children,
    meta,
}: {
    title: React.ReactNode;
    to?: string;
    badge?: React.ReactNode;
    subtitle?: React.ReactNode;
    children?: React.ReactNode;
    meta?: React.ReactNode[];
}) {
    const hasBody = subtitle || children || meta;
    const content = (
        <>
            <div data-has-body={hasBody || undefined} className="flex items-center justify-between data-has-body:mb-3">
                <span className="font-medium truncate tabular-nums slashed-zero">
                    {title}
                </span>
                {badge}
            </div>

            {subtitle && (
                <div className="text-xs font-mono text-muted-foreground truncate mb-3">
                    {subtitle}
                </div>
            )}

            {children && <div className="mb-3">{children}</div>}

            {meta && meta.length > 0 && (
                <ul className="flex items-center gap-3 text-xs text-muted-foreground">
                    {meta.map((item, index) => (
                        <li key={index}>{item}</li>
                    ))}
                </ul>
            )}
        </>
    );

    if (to) {
        return (
            <Link to={to} className="rounded-lg border bg-card p-4 hover:border-foreground/20 hover:shadow-sm transition-all">
                {content}
            </Link>
        );
    }

    return <div className="rounded-lg border bg-card p-4">{content}</div>;
}
