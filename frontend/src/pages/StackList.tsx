import { api } from "../api/client";
import type { StackSummary } from "../api/types";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { SearchInput } from "../components/search";
import { useResourceStream } from "../hooks/useResourceStream";
import { useSearchParam } from "../hooks/useSearchParam";
import { formatBytes, formatPercentage } from "../lib/format";
import { useState, useEffect, useCallback } from "react";
import { Link } from "react-router-dom";

export default function StackList() {
  const [search, , setSearch] = useSearchParam("q");
  const [summaries, setSummaries] = useState<StackSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

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

  if (loading) {
    return (
      <div>
        <PageHeader title="Stacks" />
        <SkeletonTable columns={6} />
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
      <div className="mb-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder="Search stacks..."
        />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No stacks match your search" : "No stacks found"} />
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
          <span className="ml-auto rounded bg-blue-100 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
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
      <div className="mt-3 flex gap-3 border-t pt-3 text-[10px] text-muted-foreground">
        <span>{stack.serviceCount} svc</span>
        {stack.configCount > 0 && <span>{stack.configCount} cfg</span>}
        {stack.secretCount > 0 && <span>{stack.secretCount} sec</span>}
        {stack.networkCount > 0 && <span>{stack.networkCount} net</span>}
        {stack.volumeCount > 0 && <span>{stack.volumeCount} vol</span>}
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
