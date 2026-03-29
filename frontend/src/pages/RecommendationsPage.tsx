import type { Recommendation } from "@/api/types";
import EmptyState from "@/components/EmptyState";
import PageHeader from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { useRecommendations } from "@/hooks/useRecommendations";
import { applyRecommendation } from "@/lib/applyRecommendation";
import { hintIcon, severityStyles } from "@/lib/sizingUtils";
import { getErrorMessage } from "@/lib/utils";
import { Check, Loader2, Wrench } from "lucide-react";
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

const filterGroups: Record<string, Set<string>> = {
  sizing: new Set([
    "over-provisioned",
    "approaching-limit",
    "at-limit",
    "no-limits",
    "no-reservations",
  ]),
  config: new Set(["no-healthcheck", "no-restart-policy"]),
  operational: new Set(["flaky-service", "node-disk-full", "node-memory-pressure"]),
  cluster: new Set(["single-replica", "manager-has-workloads", "uneven-distribution"]),
};

const filterTabs = ["all", "sizing", "config", "operational", "cluster"] as const;
type FilterTab = (typeof filterTabs)[number];

const filterLabels: Record<FilterTab, string> = {
  all: "All",
  sizing: "Sizing",
  config: "Config",
  operational: "Operational",
  cluster: "Cluster",
};

function targetLink(hint: Recommendation): string | null {
  if (hint.scope === "service") {
    return `/services/${hint.targetId}`;
  }

  if (hint.scope === "node") {
    return `/nodes/${hint.targetId}`;
  }

  return null;
}

interface CardProps {
  hint: Recommendation;
  originalIndex: number;
  dismissed: Set<number>;
  applying: number | null;
  onApply: (hint: Recommendation, originalIndex: number) => void;
}

function RecommendationCard({ hint, originalIndex, dismissed, applying, onApply }: CardProps) {
  if (dismissed.has(originalIndex)) {
    return null;
  }

  const CategoryIcon = hintIcon(hint.category);
  const hasFix = hint.fixAction != null && hint.suggested != null;
  const isApplying = applying === originalIndex;
  const isDone = dismissed.has(originalIndex);
  const link = targetLink(hint);

  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border bg-card px-4 py-3">
      <div className="flex items-start gap-3">
        <span
          className={`mt-1 size-2 shrink-0 rounded-full ${
            hint.severity === "critical"
              ? "bg-red-500"
              : hint.severity === "warning"
                ? "bg-amber-500"
                : "bg-blue-500"
          }`}
        />

        <CategoryIcon className={`mt-0.5 size-4 shrink-0 ${severityStyles[hint.severity]}`} />

        <div className="space-y-0.5">
          <p className="text-sm font-medium">{hint.message}</p>

          {link ? (
            <Link
              to={link}
              className="text-xs text-muted-foreground hover:underline"
            >
              {hint.targetName}
            </Link>
          ) : (
            <span className="text-xs text-muted-foreground">{hint.targetName}</span>
          )}
        </div>
      </div>

      {hasFix && !isDone && (
        <Button
          variant="outline"
          size="xs"
          disabled={applying !== null}
          onClick={() => onApply(hint, originalIndex)}
        >
          {isApplying ? <Loader2 className="size-3 animate-spin" /> : <Wrench className="size-3" />}
          Apply suggested value
        </Button>
      )}

      {isDone && (
        <span className="flex items-center gap-1 text-xs text-muted-foreground">
          <Check className="size-3" />
          Applied
        </span>
      )}
    </div>
  );
}

export default function RecommendationsPage() {
  const { items } = useRecommendations();
  const [searchParams, setSearchParams] = useSearchParams();
  const [applying, setApplying] = useState<number | null>(null);
  const [dismissed, setDismissed] = useState<Set<number>>(new Set());
  const [error, setError] = useState<string | null>(null);

  const rawFilter = searchParams.get("filter") ?? "all";
  const activeFilter: FilterTab = filterTabs.includes(rawFilter as FilterTab)
    ? (rawFilter as FilterTab)
    : "all";

  function setFilter(tab: FilterTab) {
    setSearchParams(
      (previous) => {
        const next = new URLSearchParams(previous);

        if (tab === "all") {
          next.delete("filter");
        } else {
          next.set("filter", tab);
        }

        return next;
      },
      { replace: true },
    );
  }

  const filteredItems = items
    .map((hint, index) => ({ hint, index }))
    .filter(({ hint, index }) => {
      if (dismissed.has(index)) {
        return false;
      }

      if (activeFilter === "all") {
        return true;
      }

      return filterGroups[activeFilter]?.has(hint.category) ?? false;
    });

  async function handleApply(hint: Recommendation, originalIndex: number) {
    setApplying(originalIndex);
    setError(null);

    try {
      await applyRecommendation(hint);
      setDismissed((previous) => new Set([...previous, originalIndex]));
    } catch (caughtError) {
      setError(getErrorMessage(caughtError, "Failed to apply suggestion"));
    } finally {
      setApplying(null);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader title="Recommendations" />

      <div className="flex gap-1 border-b">
        {filterTabs.map((tab) => {
          const isActive = tab === activeFilter;

          return (
            <button
              key={tab}
              onClick={() => setFilter(tab)}
              className={`px-3 py-2 text-sm transition-colors ${
                isActive
                  ? "border-b-2 border-foreground font-medium text-foreground"
                  : "text-muted-foreground hover:text-foreground"
              }`}
            >
              {filterLabels[tab]}
            </button>
          );
        })}
      </div>

      {error && <p className="text-sm font-medium text-red-700 dark:text-red-400">{error}</p>}

      {filteredItems.length === 0 ? (
        <EmptyState message="No recommendations — your cluster looks healthy" />
      ) : (
        <div className="space-y-2">
          {filteredItems.map(({ hint, index }) => (
            <RecommendationCard
              key={index}
              hint={hint}
              originalIndex={index}
              dismissed={dismissed}
              applying={applying}
              onApply={handleApply}
            />
          ))}
        </div>
      )}
    </div>
  );
}
