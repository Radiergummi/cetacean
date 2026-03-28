import { api } from "@/api/client";
import type { SizingRecommendation, SizingSeverity } from "@/api/types";
import { formatBytes, formatCores } from "@/lib/format";
import { getErrorMessage } from "@/lib/utils";
import { ArrowUp, Info, Loader2, TrendingDown, TriangleAlert, Wrench } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useState } from "react";

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

  return TrendingDown;
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
  serviceId: string;
  hints: SizingRecommendation[];
  canFix: boolean;
  onFixed?: () => void;
}

/**
 * Builds a merge-patch body for a single sizing hint's suggested value.
 */
function buildPatch(hint: SizingRecommendation): Record<string, unknown> | null {
  if (hint.suggested == null) {
    return null;
  }

  if (hint.resource === "cpu") {
    if (hint.category === "over-provisioned") {
      return { Reservations: { NanoCPUs: Math.round(hint.suggested) } };
    }

    return { Limits: { NanoCPUs: Math.round(hint.suggested) } };
  }

  if (hint.resource === "memory") {
    if (hint.category === "over-provisioned") {
      return { Reservations: { MemoryBytes: Math.round(hint.suggested) } };
    }

    return { Limits: { MemoryBytes: Math.round(hint.suggested) } };
  }

  return null;
}

/**
 * Full-width banner showing all sizing hints for a service, with
 * detailed messages, suggested values, and Fix buttons.
 */
export function SizingBanner({ serviceId, hints, canFix, onFixed }: Props) {
  const [applying, setApplying] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (hints.length === 0) {
    return null;
  }

  async function applyFix(hint: SizingRecommendation, index: number) {
    const patch = buildPatch(hint);

    if (!patch) {
      return;
    }

    setApplying(index);
    setError(null);

    try {
      await api.patchServiceResources(serviceId, patch);
      onFixed?.();
    } catch (error) {
      setError(getErrorMessage(error, "Failed to apply suggestion"));
    } finally {
      setApplying(null);
    }
  }

  const severity = highestSeverity(hints);

  return (
    <div className={`flex items-start gap-3 rounded-lg border px-4 py-3 ${bannerStyles[severity]}`}>
      <Info className={`mt-0.5 size-5 shrink-0 ${iconStyles[severity]}`} />

      <div className="flex-1 space-y-2">
        {hints.map((hint, index) => {
          const HintIcon = hintIcon(hint.category);
          const suggestion = formatSuggestion(hint);
          const patch = buildPatch(hint);
          const isApplying = applying === index;

          return (
            <div
              key={index}
              className="flex items-start justify-between gap-4"
            >
              <div className="flex items-start gap-2">
                <HintIcon className="mt-0.5 size-4 shrink-0 opacity-60" />

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

              {canFix && patch && (
                <button
                  type="button"
                  className="inline-flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-xs font-medium opacity-75 transition-opacity hover:opacity-100 disabled:opacity-50"
                  disabled={applying !== null}
                  onClick={() => applyFix(hint, index)}
                >
                  {isApplying ? (
                    <Loader2 className="size-3 animate-spin" />
                  ) : (
                    <Wrench className="size-3" />
                  )}
                  Fix
                </button>
              )}
            </div>
          );
        })}

        {error && <p className="text-xs font-medium text-red-700 dark:text-red-400">{error}</p>}
      </div>
    </div>
  );
}
