import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { CronExpressionParser } from "cron-parser";
import { Clock } from "lucide-react";
import { useMemo } from "react";

/**
 * Renders a cron expression with a tooltip showing the next 5 occurrences.
 */
export function CronSchedule({ expression }: { expression: string }) {
  const nextOccurrences = useMemo(() => {
    try {
      const interval = CronExpressionParser.parse(expression);
      const dates: Date[] = [];

      for (let i = 0; i < 5; i++) {
        dates.push(interval.next().toDate());
      }

      return dates;
    } catch {
      return null;
    }
  }, [expression]);

  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="font-mono">{expression}</span>

      {nextOccurrences && (
        <Tooltip>
          <TooltipTrigger
            render={
              <Clock className="size-3.5 text-muted-foreground/50 hover:text-muted-foreground" />
            }
          />
          <TooltipContent>
            <div className="flex flex-col gap-0.5 text-xs">
              <span className="font-medium">Next occurrences</span>
              {nextOccurrences.map((date) => (
                <span
                  key={date.getTime()}
                  className="font-mono tabular-nums"
                >
                  {date.toLocaleString()}
                </span>
              ))}
            </div>
          </TooltipContent>
        </Tooltip>
      )}
    </span>
  );
}
