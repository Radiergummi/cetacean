import { api } from "@/api/client";
import type { SizingRecommendation } from "@/api/types";
import { Button } from "@/components/ui/button";
import { formatBytes, formatCores } from "@/lib/format";
import { bannerStyles, highestSeverity, hintIcon, severityStyles } from "@/lib/sizingUtils";
import { getErrorMessage } from "@/lib/utils";
import { Info, Loader2, Wrench } from "lucide-react";
import { useState } from "react";

/**
 * Builds a merge-patch body for a single sizing hint's suggested value.
 * Over-provisioned patches the reservation; limit-based hints patch the limit.
 */
function buildPatch(hint: SizingRecommendation): Record<string, unknown> | null {
  if (hint.suggested == null) {
    return null;
  }

  const isOverProvisioned = hint.category === "over-provisioned";
  const field = isOverProvisioned ? "Reservations" : "Limits";
  const key = hint.resource === "memory" ? "MemoryBytes" : "NanoCPUs";

  return { [field]: { [key]: Math.round(hint.suggested) } };
}

function formatSuggestion(hint: SizingRecommendation): string | null {
  if (hint.suggested == null) {
    return null;
  }

  const target = hint.category === "over-provisioned" ? "reservation" : "limit";
  const value =
    hint.resource === "memory"
      ? formatBytes(hint.suggested)
      : formatCores(hint.suggested / 1e9);

  return `Suggested: ${hint.resource === "memory" ? "memory" : "CPU"} ${target} ${value}`;
}

interface Props {
  serviceId: string;
  hints: SizingRecommendation[];
  canFix: boolean;
  onFixed?: () => void;
}

/**
 * Full-width banner showing all sizing hints for a service, with
 * detailed messages, suggested values, and apply buttons.
 */
export function SizingBanner({ serviceId, hints, canFix, onFixed }: Props) {
  const [applying, setApplying] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (hints.length === 0) {
    return null;
  }

  async function applySuggestion(hint: SizingRecommendation, index: number) {
    const patch = buildPatch(hint);

    if (!patch) {
      return;
    }

    setApplying(index);
    setError(null);

    try {
      await api.patchServiceResources(serviceId, patch);
      onFixed?.();
    } catch (caughtError) {
      setError(getErrorMessage(caughtError, "Failed to apply suggestion"));
    } finally {
      setApplying(null);
    }
  }

  const severity = highestSeverity(hints);

  return (
    <div className={`flex items-start gap-3 rounded-lg border px-4 py-3 ${bannerStyles[severity]}`}>
      <Info className={`mt-0.5 size-5 shrink-0 ${severityStyles[severity]}`} />

      <div className="flex-1 space-y-2">
        {hints.map((hint, index) => {
          const HintIcon = hintIcon(hint.category);
          const suggestion = formatSuggestion(hint);
          const hasPatch = hint.suggested != null;
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

                  {suggestion && <p className="text-xs opacity-75">{suggestion}</p>}
                </div>
              </div>

              {canFix && hasPatch && (
                <Button
                  variant="outline"
                  size="xs"
                  disabled={applying !== null}
                  onClick={() => applySuggestion(hint, index)}
                >
                  {isApplying ? (
                    <Loader2 className="size-3 animate-spin" />
                  ) : (
                    <Wrench className="size-3" />
                  )}
                  Apply suggested value
                </Button>
              )}
            </div>
          );
        })}

        {error && <p className="text-xs font-medium text-red-700 dark:text-red-400">{error}</p>}
      </div>
    </div>
  );
}
