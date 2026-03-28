import { api } from "../api/client";
import type { StackSummary } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { useResourceStream } from "../hooks/useResourceStream";
import { useSearchParam } from "../hooks/useSearchParam";
import { useViewMode } from "../hooks/useViewMode";
import { formatBytes, formatPercentage } from "../lib/format";
import { useState, useEffect, useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";

function HealthDot({ health }: { health: "healthy" | "warning" | "critical" }) {
  return (
    <span
      data-health={health}
      className="inline-block size-2.5 shrink-0 rounded-full bg-yellow-500 data-[health=critical]:bg-red-500 data-[health=healthy]:bg-green-500"
    />
  );
}

function TaskHealth({ stack }: { stack: StackSummary }) {
  const running = stack.tasksByState["running"] ?? 0;
  const desired = stack.desiredTasks;
  const healthy = running >= desired && desired > 0;

  return (
    <span
      data-healthy={healthy || undefined}
      className="font-medium text-red-600 tabular-nums data-healthy:text-green-600 dark:text-red-400 dark:data-healthy:text-green-400"
    >
      {running}/{desired}
    </span>
  );
}

export default function StackList() {
  const [search, , setSearch] = useSearchParam("q");
  const [summaries, setSummaries] = useState<StackSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [viewMode, setViewMode] = useViewMode("stacks");
  const navigate = useNavigate();

  const load = useCallback(() => {
    api
      .stacksSummary()
      .then((data) => {
        setSummaries(data);
        setError(null);
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useResourceStream(
    "/stacks",
    useCallback(() => {
      load();
    }, [load]),
  );

  const filtered = search
    ? summaries.filter(({ name }) => name.toLowerCase().includes(search.toLowerCase()))
    : summaries;

  const columns: Column<StackSummary>[] = [
    {
      header: "Name",
      cell: (stack) => (
        <span className="flex items-center gap-2">
          <HealthDot health={stackHealth(stack)} />
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
      cell: (stack) => <TaskHealth stack={stack} />,
    },
    {
      header: "Services",
      cell: ({ serviceCount }) => serviceCount,
    },
    {
      header: "Status",
      cell: ({ updatingServices }) =>
        updatingServices > 0 ? (
          <span className="text-sm font-medium text-blue-600 dark:text-blue-400">
            Updating {updatingServices}
          </span>
        ) : (
          <span className="text-sm font-medium text-green-600 dark:text-green-400">Stable</span>
        ),
    },
  ];

  if (loading) {
    return (
      <div>
        <PageHeader title="Stacks" />
        <SkeletonTable columns={4} />
      </div>
    );
  }

  if (error) {
    return (
      <FetchError
        message={error.message}
        onRetry={load}
      />
    );
  }

  return (
    <div>
      <PageHeader title="Stacks" />
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search stacks…"
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No stacks match your search" : "No stacks found"} />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={filtered}
          keyFn={({ name }) => name}
          onRowClick={({ name }) => navigate(`/stacks/${name}`)}
        />
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {filtered.map((stack) => (
            <StackCard
              key={stack.name}
              stack={stack}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function stackHealth(stack: StackSummary): "healthy" | "warning" | "critical" {
  const running = stack.tasksByState["running"] ?? 0;

  if (running < stack.desiredTasks) {
    // Only critical if there are failed tasks AND we're not fully running
    if ((stack.tasksByState["failed"] ?? 0) > 0 || (stack.tasksByState["rejected"] ?? 0) > 0) {
      return "critical";
    }

    return "warning";
  }

  return "healthy";
}

function StackCard({ stack }: { stack: StackSummary }) {
  const health = stackHealth(stack);
  const running = stack.tasksByState["running"] ?? 0;
  const desired = stack.desiredTasks;
  const percentage = desired > 0 ? Math.min((running / desired) * 100, 100) : 0;

  return (
    <Link
      to={`/stacks/${stack.name}`}
      data-health={health}
      className={
        "group block rounded-lg border p-4 transition-all hover:border-foreground/20 hover:shadow-sm " +
        "data-[health=critical]:border-red-200 data-[health=critical]:bg-red-50/50 " +
        "dark:data-[health=critical]:border-red-900 dark:data-[health=critical]:bg-red-950/20"
      }
    >
      {/* Header */}
      <div className="mb-3 flex items-center gap-2">
        <span className="h-2.5 w-2.5 shrink-0 rounded-full bg-yellow-500 group-data-[health=critical]:bg-red-500 group-data-[health=healthy]:bg-green-500" />
        <span className="truncate font-medium">{stack.name}</span>
        {stack.updatingServices > 0 && (
          <span className="ms-auto rounded bg-blue-100 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
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
              className="bg-yellow-500 transition-all data-complete:bg-green-500"
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
  const color = percentage > 90 ? "bg-red-500" : percentage > 70 ? "bg-yellow-500" : "bg-blue-500";

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
