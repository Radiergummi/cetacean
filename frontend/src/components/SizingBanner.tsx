import type { SizingRecommendation, SizingSeverity } from "@/api/types";
import { formatBytes, formatCores } from "@/lib/format";
import { ArrowDown, ArrowUp, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";

const severityRank: Record<SizingSeverity, number> = {
  critical: 3,
  warning: 2,
  info: 1,
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

function highestSeverity(hints: SizingRecommendation[]): SizingSeverity {
  let max: SizingSeverity = "info";

  for (const { severity } of hints) {
    if (severityRank[severity] > severityRank[max]) {
      max = severity;
    }
  }

  return max;
}

const bannerStyles: Record<SizingSeverity, string> = {
  critical:
    "border-red-300 bg-red-50 text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200",
  warning:
    "border-amber-300 bg-amber-50 text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200",
  info: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-200",
};

const iconStyles: Record<SizingSeverity, string> = {
  critical: "text-red-600 dark:text-red-400",
  warning: "text-amber-600 dark:text-amber-400",
  info: "text-blue-600 dark:text-blue-400",
};

function formatSuggestion(hint: SizingRecommendation): string | null {
  if (hint.suggested == null) {
    return null;
  }

  if (hint.resource === "memory") {
    return formatBytes(hint.suggested);
  }

  return formatCores(hint.suggested / 1e9);
}

interface Props {
  hints: SizingRecommendation[];
  onScrollToResources?: () => void;
}

/**
 * Full-width banner showing all sizing hints for a service, with
 * detailed messages and suggested values.
 */
export function SizingBanner({ hints, onScrollToResources }: Props) {
  if (hints.length === 0) {
    return null;
  }

  const severity = highestSeverity(hints);
  const Icon = hintIcon(hints[0].category);

  return (
    <div className={`flex items-start gap-3 rounded-lg border px-4 py-3 ${bannerStyles[severity]}`}>
      <Icon className={`mt-0.5 size-5 shrink-0 ${iconStyles[severity]}`} />

      <div className="flex-1 space-y-2">
        {hints.map((hint, index) => {
          const HintIcon = hintIcon(hint.category);
          const suggestion = formatSuggestion(hint);

          return (
            <div
              key={index}
              className="flex items-start justify-between gap-4"
            >
              <div className="flex items-start gap-2">
                {hints.length > 1 && <HintIcon className="mt-0.5 size-4 shrink-0 opacity-60" />}

                <div>
                  <p className="text-sm font-medium">{hint.message}</p>

                  {suggestion && (
                    <p className="text-xs opacity-75">
                      Suggested: {hint.resource === "memory" ? "memory limit" : "CPU limit"}{" "}
                      {suggestion}
                    </p>
                  )}
                </div>
              </div>

              {onScrollToResources && (
                <button
                  type="button"
                  className="shrink-0 text-xs font-medium underline opacity-75 hover:opacity-100"
                  onClick={onScrollToResources}
                >
                  Edit resources
                </button>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
