import { headCapabilities } from "@/api/client";
import type { Recommendation, RecommendationCategory } from "@/api/types";
import EmptyState from "@/components/EmptyState";
import PageHeader from "@/components/PageHeader";
import ResourceName from "@/components/ResourceName";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { invalidateRecommendations, useRecommendations } from "@/hooks/useRecommendations";
import { useUrlParam } from "@/hooks/useUrlParam";
import { applyRecommendation, fixTarget } from "@/lib/applyRecommendation";
import {
  hintIcon,
  recommendationKey,
  recommendationLink,
  severityStyles,
  sizingCategories,
} from "@/lib/sizingUtils";
import { getErrorMessage } from "@/lib/utils";
import { Collapsible } from "@base-ui/react/collapsible";
import { skipToken, useQuery } from "@tanstack/react-query";
import { ChevronRight, Loader2, Wrench } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";

const categoryDetails: Record<RecommendationCategory, string> = {
  "no-healthcheck":
    "Without a health check, Docker cannot detect when a container is stuck or unresponsive. " +
    "Failed containers will keep receiving traffic instead of being replaced automatically.",
  "no-restart-policy":
    "Without a restart policy, containers that crash will stay down until manually restarted. " +
    "Setting a restart policy ensures automatic recovery from transient failures.",
  "newer-image":
    "The registry has moved this service's image tag to a newer image than the one it runs, which " +
    "Swarm pinned when the service was deployed. Update the image to deploy what the tag names now.",
  "unused-version":
    "Rotation replaced this config or secret with a newer version, and no service uses it any " +
    "more. Remove it once nothing deployed from a Compose file still names it.",
  "single-replica":
    "Running a single replica means any update, crash, or node failure causes downtime. " +
    "At least two replicas allow rolling updates and provide basic fault tolerance.",
  "manager-has-workloads":
    "Manager nodes handle Raft consensus and cluster coordination. Running workloads on them " +
    "can cause resource contention that affects cluster stability. Set manager availability to Drain.",
  "uneven-distribution":
    "An uneven task distribution means some nodes are overloaded while others sit idle. " +
    "This can be caused by placement constraints, resource reservations, or stale task assignments.",
  "single-manager":
    "With one manager, losing it leaves the swarm unable to schedule, update or recover anything " +
    "until it returns. Promote two workers so three managers can tolerate one failure.",
  "even-managers":
    "Raft needs a majority of managers, so an even number tolerates no more failures than one " +
    "fewer, while adding one more node that can fail. Run an odd number: three, five or seven.",
  "quorum-at-risk":
    "Too few managers are reachable to survive another loss. Without quorum the swarm keeps its " +
    "tasks running but cannot schedule, update or recover them. Bring the managers back first.",
  "network-addresses-low":
    "An overlay network that runs out of addresses fails new tasks. A start-first rolling update " +
    "needs one more address per task it starts, so a busy network runs out mid-deploy. Recreate " +
    "the network with a larger subnet, or move services onto a network of their own.",
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
  config: new Set(["no-healthcheck", "no-restart-policy", "newer-image", "unused-version"]),
  operational: new Set(["flaky-service", "node-disk-full", "node-memory-pressure"]),
  cluster: new Set([
    "single-replica",
    "manager-has-workloads",
    "uneven-distribution",
    "single-manager",
    "even-managers",
    "quorum-at-risk",
    "network-addresses-low",
  ]),
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
  // A module-level Lucide component out of a lookup, not a new one — see the
  // same binding in components/SizingBadge.tsx.
  // oxlint-disable-next-line react/static-components
  const CategoryIcon = hintIcon(hint.category);
  const fix = fixTarget(hint);
  // The target's edit links say whether this caller may apply the fix: the
  // operations level and the grant both have to admit the sub-resource.
  const { data: target } = useQuery({
    queryKey: ["capabilities", fix?.path],
    queryFn: fix != null && hint.suggested != null ? () => headCapabilities(fix.path) : skipToken,
    staleTime: 60_000,
  });
  const hasFix =
    fix != null && hint.suggested != null && (target?.editable.has(fix.subResource) ?? false);
  const isApplying = applying === recommendationKey(hint);
  const link = recommendationLink(hint);
  const detail = categoryDetails[hint.category];

  return (
    <Collapsible.Root className="rounded-lg border bg-card">
      <div className="flex items-start justify-between gap-4 px-4 py-3">
        <div className="flex min-w-0 items-start gap-3">
          {/* oxlint-disable-next-line react/static-components */}
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
  const [applying, setApplying] = useState<string | null>(null);
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());
  const [error, setError] = useState<string | null>(null);
  const [pendingDrain, setPendingDrain] = useState<Recommendation | null>(null);

  const [rawFilter, setFilter] = useUrlParam("filter", "all");
  const activeFilter: FilterTab = filterTabs.includes(rawFilter as FilterTab)
    ? (rawFilter as FilterTab)
    : "all";

  const filteredItems = items.filter((hint) => {
    if (dismissed.has(recommendationKey(hint))) {
      return false;
    }

    if (activeFilter === "all") {
      return true;
    }

    return filterGroups[activeFilter]?.has(hint.category) ?? false;
  });

  function requestApply(hint: Recommendation) {
    if (hint.fixAction?.includes("/availability")) {
      setPendingDrain(hint);

      return;
    }

    void handleApply(hint);
  }

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
              type="button"
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

      {error && <p className="text-sm font-medium text-status-danger">{error}</p>}

      {filteredItems.length === 0 ? (
        <EmptyState message="No recommendations — your cluster looks healthy" />
      ) : (
        <div className="space-y-2">
          {filteredItems.map((hint) => (
            <RecommendationCard
              key={recommendationKey(hint)}
              hint={hint}
              applying={applying}
              onApply={requestApply}
            />
          ))}
        </div>
      )}

      <AlertDialog
        open={pendingDrain !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingDrain(null);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Drain this node?</AlertDialogTitle>
            <AlertDialogDescription>
              Draining {pendingDrain?.targetName} will reschedule all running tasks to other nodes.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingDrain) {
                  void handleApply(pendingDrain);
                }

                setPendingDrain(null);
              }}
            >
              Drain
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
