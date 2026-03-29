import type { Recommendation } from "@/api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { hintIcon, severityRank, severityStyles } from "@/lib/sizingUtils";
import { Check } from "lucide-react";

/**
 * Compact label for the table column (e.g., "CPU 85%", "No limits").
 */
function formatCompactLabel(hint: Recommendation): string {
  if (hint.category === "no-limits") {
    return "No limits";
  }

  if (hint.category === "no-reservations") {
    return "No reservations";
  }

  const percentage = Math.round((hint.current / hint.configured) * 100);

  return `${hint.resource.toUpperCase()} ${percentage}%`;
}

/**
 * Displays the highest-severity sizing hint for a service in the table column.
 * Shows green check when there are no hints.
 * Wraps in a tooltip listing all hints when there are multiple.
 */
export function SizingBadge({ hints }: { hints: Recommendation[] }) {
  if (hints.length === 0) {
    return (
      <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
        <Check className="size-3.5" />
        OK
      </span>
    );
  }

  let top = hints[0];

  for (const hint of hints) {
    if (severityRank[hint.severity] > severityRank[top.severity]) {
      top = hint;
    }
  }

  const Icon = hintIcon(top.category);

  const badge = (
    <span className={`inline-flex items-center gap-1 ${severityStyles[top.severity]}`}>
      <Icon className="size-3.5" />
      {formatCompactLabel(top)}
    </span>
  );

  if (hints.length <= 1) {
    return badge;
  }

  return (
    <Tooltip>
      <TooltipTrigger render={badge} />
      <TooltipContent>
        <ul className="space-y-1">
          {hints.map((hint, index) => (
            <li key={index}>{hint.message}</li>
          ))}
        </ul>
      </TooltipContent>
    </Tooltip>
  );
}
