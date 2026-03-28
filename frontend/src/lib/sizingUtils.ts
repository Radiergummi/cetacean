import type { SizingRecommendation, SizingSeverity } from "@/api/types";
import { ArrowUp, TrendingDown, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";

export const severityRank: Record<SizingSeverity, number> = {
  critical: 3,
  warning: 2,
  info: 1,
};

export const severityStyles: Record<SizingSeverity, string> = {
  critical: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400",
  info: "text-blue-600 dark:text-blue-400",
};

export const bannerStyles: Record<SizingSeverity, string> = {
  critical:
    "border-red-300 bg-red-50 text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200",
  warning:
    "border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200",
  info: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-200",
};

export const iconStyles: Record<SizingSeverity, string> = {
  critical: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400",
  info: "text-blue-600 dark:text-blue-400",
};

export function hintIcon(category: SizingRecommendation["category"]): LucideIcon {
  if (category === "no-limits" || category === "no-reservations") {
    return TriangleAlert;
  }

  if (category === "at-limit" || category === "approaching-limit") {
    return ArrowUp;
  }

  return TrendingDown;
}

export function highestSeverity(hints: SizingRecommendation[]): SizingSeverity {
  let max: SizingSeverity = "info";

  for (const { severity } of hints) {
    if (severityRank[severity] > severityRank[max]) {
      max = severity;
    }
  }

  return max;
}
