import type { SizingRecommendation, SizingSeverity } from "@/api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const severityRank: Record<SizingSeverity, number> = {
  critical: 3,
  warning: 2,
  info: 1,
};

const severityStyles: Record<SizingSeverity, string> = {
  critical: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400",
  info: "text-blue-600 dark:text-blue-400",
};

function hintIcon(category: SizingRecommendation["category"]): string {
  if (category === "no-limits" || category === "no-reservations") {
    return "☐";
  }

  if (category === "at-limit" || category === "approaching-limit") {
    return "▲";
  }

  return "▼";
}

function formatHintLabel(hint: SizingRecommendation): string {
  const icon = hintIcon(hint.category);

  if (hint.category === "no-limits") {
    return `${icon} No limits`;
  }

  if (hint.category === "no-reservations") {
    return `${icon} No reservations`;
  }

  const percentage = Math.round(hint.current * 100);

  return `${icon} ${hint.resource.toUpperCase()} ${percentage}%`;
}

/**
 * Returns the highest-severity hint plus label and all hints,
 * or null if the hints array is empty.
 */
export function highestSeverityHint(
  hints: SizingRecommendation[],
): { label: string; severity: SizingSeverity; allHints: SizingRecommendation[] } | null {
  if (hints.length === 0) {
    return null;
  }

  const sorted = [...hints].sort(
    (first, second) => severityRank[second.severity] - severityRank[first.severity],
  );
  const top = sorted[0];

  return {
    label: formatHintLabel(top),
    severity: top.severity,
    allHints: hints,
  };
}

/**
 * Displays the highest-severity sizing hint for a service.
 * Shows green "✓ OK" when there are no hints.
 * Wraps in a tooltip listing all hints when there are multiple.
 */
export function SizingBadge({ hints }: { hints: SizingRecommendation[] }) {
  const top = highestSeverityHint(hints);

  if (top === null) {
    return <span className="text-green-600 dark:text-green-400">✓ OK</span>;
  }

  const badge = (
    <span className={severityStyles[top.severity]}>{top.label}</span>
  );

  if (top.allHints.length <= 1) {
    return badge;
  }

  return (
    <Tooltip>
      <TooltipTrigger render={badge} />
      <TooltipContent>
        <ul className="space-y-1">
          {top.allHints.map((hint, index) => (
            <li key={index}>{hint.message}</li>
          ))}
        </ul>
      </TooltipContent>
    </Tooltip>
  );
}
