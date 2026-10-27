import type { ServiceLink } from "@/api/types";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ExternalLink } from "lucide-react";

/**
 * Custom links declared through `cetacean.link.*` service labels. Only an
 * entry the server gave an `href` is rendered as a link. The caller renders
 * nothing at all when there are no links.
 */
export default function ServiceLinks({ links }: { links: ServiceLink[] }) {
  return (
    <ul
      aria-label="Links"
      className="flex max-w-full min-w-0 flex-wrap items-center gap-2"
    >
      {links.map((link) => {
        const { id, title, href, text } = link;
        const label = disambiguatedLabel(link, links);

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

/**
 * A title shared with another link gains its id; where the ids collide too,
 * as on a stack whose services declare the same link, the declaring services.
 */
function disambiguatedLabel({ id, title, services }: ServiceLink, links: ServiceLink[]) {
  const sameTitle = links.filter((link) => link.title === title);

  if (sameTitle.length < 2) {
    return title;
  }

  const idsCollide = sameTitle.filter((link) => link.id === id).length > 1;

  if (idsCollide && services?.length) {
    return `${title} · ${services.join(", ")}`;
  }

  return `${title} · ${id}`;
}
