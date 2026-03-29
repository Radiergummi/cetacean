import { api } from "@/api/client";
import type { Recommendation } from "@/api/types";
import { Button } from "@/components/ui/button";
import { formatBytes, formatCores } from "@/lib/format";
import { bannerStyles, highestSeverity, hintIcon, severityStyles } from "@/lib/sizingUtils";
import { getErrorMessage } from "@/lib/utils";
import { Info, Loader2, Wrench } from "lucide-react";
import { useState } from "react";

/**
 * Builds a patch or action for a single recommendation's suggested value.
 * Returns a function that performs the fix, or null if no fix is available.
 */
async function applyFix(hint: Recommendation, serviceId: string): Promise<void> {
  if (hint.fixAction == null || hint.suggested == null) {
    return;
  }

  if (hint.fixAction.startsWith("PATCH") && hint.fixAction.includes("/resources")) {
    const isOverProvisioned = hint.category === "over-provisioned";
    const field = isOverProvisioned ? "Reservations" : "Limits";
    const key = hint.resource === "memory" ? "MemoryBytes" : "NanoCPUs";
    const patch = { [field]: { [key]: Math.round(hint.suggested) } };
    await api.patchServiceResources(serviceId, patch);

    return;
  }

  if (hint.fixAction.startsWith("PUT") && hint.fixAction.includes("/scale")) {
    await api.scaleService(hint.targetId, Math.round(hint.suggested));

    return;
  }

  if (hint.fixAction.startsWith("PUT") && hint.fixAction.includes("/availability")) {
    await api.updateNodeAvailability(hint.targetId, "drain");

    return;
  }
}

function formatSuggestion(hint: Recommendation): string | null {
  if (hint.suggested == null) {
    return null;
  }

  const target = hint.category === "over-provisioned" ? "reservation" : "limit";
  const value =
    hint.resource === "memory" ? formatBytes(hint.suggested) : formatCores(hint.suggested / 1e9);

  return `Suggested: ${hint.resource === "memory" ? "memory" : "CPU"} ${target} ${value}`;
}

interface Props {
  serviceId: string;
  hints: Recommendation[];
  canFix: boolean;
  onFixed?: () => void;
}

/**
 * Full-width banner showing all sizing hints for a service, with
 * detailed messages, suggested values, and apply buttons.
 */
export function SizingBanner({ serviceId, hints, canFix, onFixed }: Props) {
  const [applying, setApplying] = useState<number | null>(null);
  const [dismissed, setDismissed] = useState<Set<number>>(new Set());
  const [error, setError] = useState<string | null>(null);

  const visibleHints = hints.filter((_, index) => !dismissed.has(index));

  if (visibleHints.length === 0) {
    return null;
  }

  async function applySuggestion(hint: Recommendation, originalIndex: number) {
    if (hint.fixAction == null || hint.suggested == null) {
      return;
    }

    setApplying(originalIndex);
    setError(null);

    try {
      await applyFix(hint, serviceId);
      setDismissed((previous) => new Set([...previous, originalIndex]));
      onFixed?.();
    } catch (caughtError) {
      setError(getErrorMessage(caughtError, "Failed to apply suggestion"));
    } finally {
      setApplying(null);
    }
  }

  const severity = highestSeverity(visibleHints);

  return (
    <div className={`flex items-start gap-3 rounded-lg border px-4 py-3 ${bannerStyles[severity]}`}>
      <Info className={`mt-0.5 size-5 shrink-0 ${severityStyles[severity]}`} />

      <div className="flex-1 space-y-2">
        {hints.map((hint, originalIndex) => {
          if (dismissed.has(originalIndex)) {
            return null;
          }

          const HintIcon = hintIcon(hint.category);
          const suggestion = formatSuggestion(hint);
          const hasFix = hint.fixAction != null && hint.suggested != null;
          const isApplying = applying === originalIndex;

          return (
            <div
              key={originalIndex}
              className="flex items-center justify-between gap-4"
            >
              <div className="flex items-start gap-2">
                <HintIcon className="mt-0.5 size-4 shrink-0 opacity-60" />

                <div>
                  <p className="text-sm font-medium">{hint.message}</p>

                  {suggestion && <p className="text-xs opacity-75">{suggestion}</p>}
                </div>
              </div>

              {canFix && hasFix && (
                <Button
                  variant="outline"
                  size="xs"
                  disabled={applying !== null}
                  onClick={() => applySuggestion(hint, originalIndex)}
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
