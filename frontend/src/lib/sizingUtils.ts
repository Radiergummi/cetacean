import type { Recommendation, RecommendationSeverity } from "@/api/types";
import { formatBytes, formatCores } from "@/lib/format";
import { ArrowUp, Copy, RefreshCw, Scale, Shield, TrendingDown, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";

export const sizingCategories = new Set<string>([
  "over-provisioned",
  "approaching-limit",
  "at-limit",
  "no-limits",
  "no-reservations",
]);

export const severityRank: Record<RecommendationSeverity, number> = {
  critical: 3,
  warning: 2,
  info: 1,
};

export const severityStyles: Record<RecommendationSeverity, string> = {
  critical: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400",
  info: "text-blue-600 dark:text-blue-400",
};

export const bannerStyles: Record<RecommendationSeverity, string> = {
  critical:
    "border-red-300 bg-red-50 text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200",
  warning:
    "border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200",
  info: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-200",
};

export function hintIcon(category: Recommendation["category"]): LucideIcon {
  if (category === "no-limits" || category === "no-reservations") {
    return TriangleAlert;
  }

  if (category === "at-limit" || category === "approaching-limit") {
    return ArrowUp;
  }

  if (category === "no-healthcheck" || category === "no-restart-policy") {
    return TriangleAlert;
  }

  if (category === "flaky-service") {
    return RefreshCw;
  }

  if (category === "node-disk-full" || category === "node-memory-pressure") {
    return TriangleAlert;
  }

  if (category === "single-replica") {
    return Copy;
  }

  if (category === "manager-has-workloads") {
    return Shield;
  }

  if (category === "uneven-distribution") {
    return Scale;
  }

  return TrendingDown;
}

export function highestSeverity(hints: Recommendation[]): RecommendationSeverity {
  let max: RecommendationSeverity = "info";

  for (const { severity } of hints) {
    if (severityRank[severity] > severityRank[max]) {
      max = severity;
    }
  }

  return max;
}

/**
 * Format a recommendation's suggested value as a human-readable string.
 */
export function formatSuggestion(hint: Recommendation): string | null {
  if (hint.suggested == null) {
    return null;
  }

  if (hint.category === "single-replica") {
    return `Suggested: ${hint.suggested} replicas`;
  }

  if (hint.category === "manager-has-workloads") {
    return "Suggested: drain manager node";
  }

  if (!hint.resource) {
    return null;
  }

  const target = hint.category === "over-provisioned" ? "reservation" : "limit";
  const value =
    hint.resource === "memory" ? formatBytes(hint.suggested) : formatCores(hint.suggested / 1e9);

  return `Suggested: ${hint.resource === "memory" ? "memory" : "CPU"} ${target} ${value}`;
}

/**
 * Build a composite key for recommendation identity (deduplication, dismissal).
 */
export function recommendationKey(hint: Recommendation): string {
  return `${hint.targetId}:${hint.category}:${hint.resource}`;
}

/**
 * Map a recommendation to its navigation path, or null for cluster-scoped hints.
 */
export function recommendationLink(hint: Recommendation): string | null {
  if (hint.scope === "service") {
    return `/services/${hint.targetId}`;
  }

  if (hint.scope === "node") {
    return `/nodes/${hint.targetId}`;
  }

  return null;
}
