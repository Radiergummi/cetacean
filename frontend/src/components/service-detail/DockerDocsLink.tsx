import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { CircleHelp } from "lucide-react";

/**
 * Tiny help icon linking to Docker documentation.
 */
export function DockerDocsLink({ href }: { href: string }) {
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
