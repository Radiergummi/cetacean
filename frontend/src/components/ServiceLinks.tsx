import type { ServiceLink } from "@/api/types";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ExternalLink } from "lucide-react";

/**
 * Custom links declared through `cetacean.link.*` service labels. Only an
 * entry the server gave an `href` is rendered as a link.
 */
export default function ServiceLinks({ links }: { links: ServiceLink[] }) {
  if (links.length === 0) {
    return null;
  }

  return (
    <ul
      aria-label="Links"
      className="flex max-w-full min-w-0 flex-wrap items-center gap-2"
    >
      {links.map(({ id, title, href, text }) => (
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
              <span className="truncate">{title}</span>
              <ExternalLink data-icon="inline-end" />
            </a>
          ) : (
            <span
              title={`${title}: ${text}`}
              className="block truncate text-[0.8rem] text-muted-foreground"
            >
              {title}: {text}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}
