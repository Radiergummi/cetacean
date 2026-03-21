import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Info } from "lucide-react";

export function HelpTooltip({ text }: { text: string }) {
  return (
    <Tooltip>
      <TooltipTrigger render={<Info className="size-3.5 text-muted-foreground/50" />} />
      <TooltipContent>{text}</TooltipContent>
    </Tooltip>
  );
}
