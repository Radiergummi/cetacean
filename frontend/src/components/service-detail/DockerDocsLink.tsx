import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { CircleHelp, ExternalLink } from "lucide-react";

/**
 * Link to Docker documentation. Defaults to a tiny help icon with tooltip;
 * use variant="label" for a visible "Docs" text link with an external-link icon.
 */
export function DockerDocsLink({
  href,
  variant = "icon",
}: {
  href: string;
  variant?: "icon" | "label";
}) {
  if (variant === "label") {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        onClick={(event) => event.stopPropagation()}
      >
        Docs
        <ExternalLink className="size-3" />
      </a>
    );
  }

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex text-muted-foreground hover:text-foreground"
            onClick={(event) => event.stopPropagation()}
          >
            <CircleHelp className="size-3" />
          </a>
        }
      />
      <TooltipContent>Docker docs</TooltipContent>
    </Tooltip>
  );
}
