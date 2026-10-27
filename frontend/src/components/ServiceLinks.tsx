import type { ServiceLink } from "@/api/types";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ExternalLink } from "lucide-react";

/**
 * Custom links declared through `cetacean.link.*` service labels. Only an
 * entry the server gave an `href` is rendered as a link. Where titles collide,
 * a stack's links name the services declaring them.
 */
export default function ServiceLinks({ links }: { links: ServiceLink[] }) {
  if (links.length === 0) {
    return null;
  }

  const titleCounts = new Map<string, number>();

  for (const { title } of links) {
    titleCounts.set(title, (titleCounts.get(title) ?? 0) + 1);
  }

  return (
    <ul
      aria-label="Links"
      className="flex max-w-full min-w-0 flex-wrap items-center gap-2"
    >
      {links.map(({ id, title, href, text, services }) => {
        const label =
          (titleCounts.get(title) ?? 0) > 1 && services?.length
            ? `${title} · ${services.join(", ")}`
            : title;

        return (
          <li
            key={JSON.stringify([id, title, href, text])}
            className="max-w-full min-w-0"
          >
            {href ? (
              <a
                href={href}
                target="_blank"
                rel="noopener noreferrer"
                title={href}
                className={cn(buttonVariants({ variant: "outline", size: "sm" }), "max-w-full")}
              >
                <span className="truncate">{label}</span>
                <ExternalLink data-icon="inline-end" />
              </a>
            ) : (
              <span
                title={`${label}: ${text}`}
                className="block truncate text-[0.8rem] text-muted-foreground"
              >
                {label}: {text}
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}
