import type { ServiceLink } from "@/api/types";
import { buttonVariants } from "@/components/ui/button";
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
    <div className="flex flex-wrap items-center gap-2">
      {links.map(({ id, title, href, text }) =>
        href ? (
          <a
            key={`${id}:${href}`}
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            {title}
            <ExternalLink data-icon="inline-end" />
          </a>
        ) : (
          <span
            key={`${id}:${text}`}
            className="text-[0.8rem] text-muted-foreground"
          >
            {title}: {text}
          </span>
        ),
      )}
    </div>
  );
}
