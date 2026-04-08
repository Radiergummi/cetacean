import type { Recommendation, RecommendationCategory } from "@/api/types";
import EmptyState from "@/components/EmptyState";
import PageHeader from "@/components/PageHeader";
import ResourceName from "@/components/ResourceName";
import { Button } from "@/components/ui/button";
import { invalidateRecommendations, useRecommendations } from "@/hooks/useRecommendations";
import { applyRecommendation } from "@/lib/applyRecommendation";
import {
  hintIcon,
  recommendationKey,
  recommendationLink,
  severityStyles,
  sizingCategories,
} from "@/lib/sizingUtils";
import { getErrorMessage } from "@/lib/utils";
import { Collapsible } from "@base-ui/react/collapsible";
import { ChevronRight, Loader2, Wrench } from "lucide-react";
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

const categoryDetails: Record<RecommendationCategory, string> = {
  "no-healthcheck":
    "Without a health check, Docker cannot detect when a container is stuck or unresponsive. " +
    "Failed containers will keep receiving traffic instead of being replaced automatically.",
  "no-restart-policy":
    "Without a restart policy, containers that crash will stay down until manually restarted. " +
    "Setting a restart policy ensures automatic recovery from transient failures.",
  "single-replica":
    "Running a single replica means any update, crash, or node failure causes downtime. " +
    "At least two replicas allow rolling updates and provide basic fault tolerance.",
  "manager-has-workloads":
    "Manager nodes handle Raft consensus and cluster coordination. Running workloads on them " +
    "can cause resource contention that affects cluster stability. Set manager availability to Drain.",
  "uneven-distribution":
    "An uneven task distribution means some nodes are overloaded while others sit idle. " +
    "This can be caused by placement constraints, resource reservations, or stale task assignments.",
  "flaky-service":
    "Frequent task restarts indicate a recurring failure — OOM kills, crash loops, or failing " +
    "health checks. Investigate container logs and resource usage for the root cause.",
  "node-disk-full":
    "When a node runs out of disk space, containers cannot write logs or temporary files and new " +
    "images cannot be pulled. This typically leads to cascading task failures across the node.",
  "node-memory-pressure":
    "High memory usage on a node triggers the kernel OOM killer, which terminates containers " +
    "unpredictably. Reduce workloads on this node or add memory capacity.",
  "no-limits":
    "Without resource limits, a single service can consume all available CPU or memory on a node, " +
    "starving other services. Set limits to ensure fair resource sharing.",
  "no-reservations":
    "Reservations tell the scheduler how much capacity a service needs. Without them, Docker may " +
    "place too many services on one node, leading to overcommitment and poor performance.",
  "at-limit":
    "The service is consuming nearly all of its allocated resources. It may be throttled (CPU) " +
    "or killed (memory) under load. Consider increasing the limit or optimizing the service.",
  "approaching-limit":
    "Resource usage is trending toward the configured limit. If traffic increases, the service " +
    "may hit the limit and experience throttling or OOM kills.",
  "over-provisioned":
    "The service is using significantly less than its reserved resources. Reducing the reservation " +
    "frees capacity for other services and improves cluster utilization.",
};

const filterGroups: Record<string, Set<string>> = {
  sizing: sizingCategories,
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

interface CardProps {
  hint: Recommendation;
  applying: string | null;
  onApply: (hint: Recommendation) => void;
}

function RecommendationCard({ hint, applying, onApply }: CardProps) {
  const CategoryIcon = hintIcon(hint.category);
  const hasFix = hint.fixAction != null && hint.suggested != null;
  const isApplying = applying === recommendationKey(hint);
  const link = recommendationLink(hint);
  const detail = categoryDetails[hint.category];

  return (
    <Collapsible.Root className="rounded-lg border bg-card">
      <div className="flex items-start justify-between gap-4 px-4 py-3">
        <div className="flex min-w-0 items-start gap-3">
          <CategoryIcon
            aria-label={hint.severity}
            className={`mt-0.5 size-4 shrink-0 ${severityStyles[hint.severity]}`}
          />

          <div className="min-w-0 space-y-0.5">
            <Collapsible.Trigger className="group flex cursor-pointer items-center gap-1.5 text-left text-sm font-medium transition-colors hover:text-foreground">
              <ChevronRight className="size-3.5 shrink-0 text-muted-foreground transition-transform group-data-[panel-open]:rotate-90" />
              {hint.message}
            </Collapsible.Trigger>

            {link ? (
              <Link
                to={link}
                className="ml-5 block text-xs text-muted-foreground hover:underline"
              >
                <ResourceName name={hint.targetName} />
              </Link>
            ) : (
              <span className="ml-5 block text-xs text-muted-foreground">
                <ResourceName name={hint.targetName} />
              </span>
            )}
          </div>
        </div>

        {hasFix && (
          <Button
            variant="outline"
            size="xs"
            disabled={applying !== null}
            onClick={() => onApply(hint)}
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

      {detail && (
        <Collapsible.Panel className="overflow-hidden transition-all data-[ending-style]:h-0 data-[starting-style]:h-0">
          <p className="border-t px-4 py-3 pl-[3.25rem] text-xs leading-relaxed text-muted-foreground">
            {detail}
          </p>
        </Collapsible.Panel>
      )}
    </Collapsible.Root>
  );
}

export default function RecommendationsPage() {
  const { items } = useRecommendations();
  const [searchParams, setSearchParams] = useSearchParams();
  const [applying, setApplying] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());
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

  const filteredItems = items.filter((hint) => {
    if (dismissed.has(recommendationKey(hint))) {
      return false;
    }

    if (activeFilter === "all") {
      return true;
    }

    return filterGroups[activeFilter]?.has(hint.category) ?? false;
  });

  async function handleApply(hint: Recommendation) {
    const key = recommendationKey(hint);
    setApplying(key);
    setError(null);

    try {
      await applyRecommendation(hint);
      setDismissed((previous) => new Set([...previous, key]));
      invalidateRecommendations();
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
          {filteredItems.map((hint) => (
            <RecommendationCard
              key={recommendationKey(hint)}
              hint={hint}
              applying={applying}
              onApply={handleApply}
            />
          ))}
        </div>
      )}
    </div>
  );
}
