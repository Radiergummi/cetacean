import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { CircleHelp } from "lucide-react";

const baseUrl = "https://docs.docker.com/reference/cli/docker/service/create/";

/**
 * Tiny help icon linking to Docker CLI reference for a specific option.
 * Pass the CLI flag name as `anchor` (e.g. "hostname", "cap-add").
 */
export function DockerDocsLink({ anchor }: { anchor: string }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <a
            href={`${baseUrl}#${anchor}`}
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
