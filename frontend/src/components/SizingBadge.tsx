import type { SizingRecommendation, SizingSeverity } from "@/api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ArrowDown, ArrowUp, Check, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";

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

function hintIcon(category: SizingRecommendation["category"]): LucideIcon {
  if (category === "no-limits" || category === "no-reservations") {
    return TriangleAlert;
  }

  if (category === "at-limit" || category === "approaching-limit") {
    return ArrowUp;
  }

  return ArrowDown;
}

const categoryLabels: Record<SizingRecommendation["category"], string> = {
  "over-provisioned": "over-provisioned",
  "approaching-limit": "near limit",
  "at-limit": "at limit",
  "no-limits": "No limits",
  "no-reservations": "No reservations",
};

/**
 * Compact label for the table column (e.g., "CPU 85%").
 */
function formatCompactLabel(hint: SizingRecommendation): string {
  if (hint.category === "no-limits" || hint.category === "no-reservations") {
    return categoryLabels[hint.category];
  }

  const percentage = Math.round((hint.current / hint.configured) * 100);

  return `${hint.resource.toUpperCase()} ${percentage}%`;
}

/**
 * Descriptive label for the detail page badge (e.g., "CPU over-provisioned").
 */
function formatDescriptiveLabel(hint: SizingRecommendation): string {
  if (hint.category === "no-limits" || hint.category === "no-reservations") {
    return categoryLabels[hint.category];
  }

  return `${hint.resource.toUpperCase()} ${categoryLabels[hint.category]}`;
}

/**
 * Returns the highest-severity hint info, or null if the hints array is empty.
 * `compactText` is for the table column ("CPU 85%"), `descriptiveText` for the
 * detail page badge ("CPU near limit").
 */
export function highestSeverityHint(hints: SizingRecommendation[]): {
  Icon: LucideIcon;
  compactText: string;
  descriptiveText: string;
  severity: SizingSeverity;
  allHints: SizingRecommendation[];
} | null {
  if (hints.length === 0) {
    return null;
  }

  const sorted = [...hints].sort(
    (first, second) => severityRank[second.severity] - severityRank[first.severity],
  );
  const top = sorted[0];

  return {
    Icon: hintIcon(top.category),
    compactText: formatCompactLabel(top),
    descriptiveText: formatDescriptiveLabel(top),
    severity: top.severity,
    allHints: sorted,
  };
}

/**
 * Displays the highest-severity sizing hint for a service.
 * Shows green check when there are no hints.
 * Wraps in a tooltip listing all hints when there are multiple.
 */
export function SizingBadge({ hints }: { hints: SizingRecommendation[] }) {
  const top = highestSeverityHint(hints);

  if (top === null) {
    return (
      <span className="inline-flex items-center gap-1 text-green-600 dark:text-green-400">
        <Check className="size-3.5" />
        OK
      </span>
    );
  }

  const { Icon } = top;

  const badge = (
    <span className={`inline-flex items-center gap-1 ${severityStyles[top.severity]}`}>
      <Icon className="size-3.5" />
      {top.compactText}
    </span>
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
