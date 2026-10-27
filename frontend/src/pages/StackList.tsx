import { api } from "../api/client";
import type { StackSummary } from "../api/types";
import type { Column } from "../components/DataTable";
import { HealthDot, ReplicaHealth } from "../components/HealthIndicator";
import { ListPageView } from "../components/ResourceListPage";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { useSearchParam } from "../hooks/useSearchParam";
import { useViewMode } from "../hooks/useViewMode";
import { formatBytes, formatPercentage } from "../lib/format";
import { stackHealth } from "../lib/stackHealth";
import { cardGridClass } from "../lib/styles";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { Link } from "react-router-dom";

export default function StackList() {
  const [search, , setSearch] = useSearchParam("q");
  const {
    data: summaries = [],
    isLoading: loading,
    error,
    refetch,
  } = useQuery({
    queryKey: ["stacks-summary"],
    queryFn: () => api.stacksSummary(),
  });
  const [viewMode, setViewMode] = useViewMode("stacks");

  useDebouncedInvalidation("/stacks", [["stacks-summary"]]);

  const filtered = search
    ? summaries.filter(({ name }) => name.toLowerCase().includes(search.toLowerCase()))
    : summaries;

  const columns: Column<StackSummary>[] = useMemo(
    () => [
      {
        header: "Name",
        cell: (stack) => (
          <span className="flex items-center gap-2">
            <HealthDot health={stackHealth(stack.tasksByState, stack.desiredTasks)} />
            <Link
              to={`/stacks/${stack.name}`}
              className="font-medium text-link hover:underline"
              onClick={(event) => event.stopPropagation()}
            >
              {stack.name}
            </Link>
          </span>
        ),
      },
      {
        header: "Tasks",
        cell: (stack) => (
          <ReplicaHealth
            running={stack.tasksByState["running"] ?? 0}
            desired={stack.desiredTasks}
          />
        ),
      },
      {
        header: "Services",
        cell: ({ serviceCount }) => serviceCount,
      },
      {
        // Rollout, not health — the Tasks column is what says whether the
        // stack is running, so a settled rollout is left uncoloured here.
        header: "Rollout",
        cell: ({ updatingServices }) =>
          updatingServices > 0 ? (
            <span className="text-sm font-medium text-status-info">
              Updating {updatingServices}
            </span>
          ) : (
            <span className="text-sm font-medium text-muted-foreground">Stable</span>
          ),
      },
    ],
    [],
  );

  return (
    <ListPageView
      title="Stacks"
      searchPlaceholder="Search stacks…"
      itemPath={({ name }) => `/stacks/${name}`}
      keyFn={({ name }) => name}
      columns={columns}
      emptyMessage={(hasSearch) => (hasSearch ? "No stacks match your search" : "No stacks found")}
      renderGrid={(stacks) => (
        <div className={cardGridClass}>
          {stacks.map((stack) => (
            <StackCard
              key={stack.name}
              stack={stack}
            />
          ))}
        </div>
      )}
      data={filtered}
      loading={loading}
      error={error}
      retry={() => {
        void refetch();
      }}
      search={search}
      setSearch={setSearch}
      viewMode={viewMode}
      setViewMode={setViewMode}
    />
  );
}

function StackCard({ stack }: { stack: StackSummary }) {
  const health = stackHealth(stack.tasksByState, stack.desiredTasks);
  const running = stack.tasksByState["running"] ?? 0;
  const desired = stack.desiredTasks;
  const percentage = desired > 0 ? Math.min((running / desired) * 100, 100) : 0;

  return (
    <Link
      to={`/stacks/${stack.name}`}
      data-health={health}
      className={
        "group block rounded-lg border p-4 transition-all hover:border-foreground/20 hover:shadow-sm " +
        "data-[health=critical]:border-status-danger/30 data-[health=critical]:bg-status-danger/10"
      }
    >
      {/* Header */}
      <div className="mb-3 flex items-center gap-2">
        <HealthDot health={health} />
        <span className="truncate font-medium">{stack.name}</span>
        {stack.updatingServices > 0 && (
          <span className="ms-auto rounded bg-status-info/15 px-1.5 py-0.5 text-[10px] font-medium text-status-info">
            Updating {stack.updatingServices}
          </span>
        )}
      </div>

      {/* Task bar */}
      <div className="mb-3">
        <div className="mb-1 flex justify-between text-xs text-muted-foreground">
          <span>Tasks</span>
          <span className="tabular-nums">
            {running}/{desired}
          </span>
        </div>
        {desired > 0 ? (
          <div className="flex h-2 overflow-hidden rounded-full bg-muted">
            <div
              data-complete={percentage >= 100 || undefined}
              className="bg-status-warning transition-all data-complete:bg-status-ok"
              style={{ width: `${percentage}%` }}
            />
          </div>
        ) : (
          <div className="h-2 rounded-full bg-muted" />
        )}
      </div>

      {/* Resource bars */}
      {stack.memoryLimitBytes > 0 && (
        <ResourceBar
          label="Memory"
          used={stack.memoryUsageBytes}
          limit={stack.memoryLimitBytes}
          format={formatBytes}
        />
      )}
      {stack.cpuLimitCores > 0 && (
        <ResourceBar
          label="CPU"
          used={stack.cpuUsagePercent}
          limit={stack.cpuLimitCores * 100}
          format={(value) => formatPercentage(value, 0)}
        />
      )}

      {/* Resource counts footer */}
      <div className="mt-3 flex flex-wrap gap-1.5 border-t pt-3">
        <CountPill
          count={stack.serviceCount}
          label="services"
        />
        {stack.configCount > 0 && (
          <CountPill
            count={stack.configCount}
            label="configs"
          />
        )}
        {stack.secretCount > 0 && (
          <CountPill
            count={stack.secretCount}
            label="secrets"
          />
        )}
        {stack.networkCount > 0 && (
          <CountPill
            count={stack.networkCount}
            label="networks"
          />
        )}
        {stack.volumeCount > 0 && (
          <CountPill
            count={stack.volumeCount}
            label="volumes"
          />
        )}
      </div>
    </Link>
  );
}

function ResourceBar({
  label,
  used,
  limit,
  format,
}: {
  label: string;
  used: number;
  limit: number;
  format: (value: number) => string;
}) {
  const percentage = limit > 0 ? Math.min((used / limit) * 100, 100) : 0;
  const color =
    percentage > 90 ? "bg-status-danger" : percentage > 70 ? "bg-status-warning" : "bg-status-info";

  return (
    <div className="mb-2">
      <div className="mb-0.5 flex justify-between text-xs text-muted-foreground">
        <span>{label}</span>
        <span className="tabular-nums">
          {format(used)} / {format(limit)}
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-muted">
        <div
          className={`h-full rounded-full ${color} transition-all`}
          style={{ width: `${percentage}%` }}
        />
      </div>
    </div>
  );
}

function CountPill({ count, label }: { count: number; label: string }) {
  return (
    <span className="rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground tabular-nums">
      {count} {count === 1 ? label.replace(/s$/, "") : label}
    </span>
  );
}
