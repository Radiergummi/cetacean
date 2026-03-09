import { useState, useEffect, useCallback } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { StackSummary } from "../api/types";
import { useSSE } from "../hooks/useSSE";
import { useSearchParam } from "../hooks/useSearchParam";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { formatBytes } from "../lib/formatBytes";

export default function StackList() {
  const [search, setSearch] = useSearchParam("q");
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

  useSSE(
    ["stack", "service", "task"],
    useCallback(() => {
      load();
    }, [load]),
  );

  const filtered = search
    ? summaries.filter((s) => s.name.toLowerCase().includes(search.toLowerCase()))
    : summaries;

  if (loading)
    return (
      <div>
        <PageHeader title="Stacks" />
        <SkeletonTable columns={6} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={load} />;

  return (
    <div>
      <PageHeader title="Stacks" />
      <div className="mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search stacks..." />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No stacks match your search" : "No stacks found"} />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((stack) => (
            <StackCard key={stack.name} stack={stack} />
          ))}
        </div>
      )}
    </div>
  );
}

function stackHealth(s: StackSummary): "healthy" | "warning" | "critical" {
  if ((s.tasksByState["failed"] ?? 0) > 0) return "critical";
  const running = s.tasksByState["running"] ?? 0;
  if (running < s.desiredTasks) return "warning";
  return "healthy";
}

const HEALTH_COLORS = {
  healthy: "bg-green-500",
  warning: "bg-yellow-500",
  critical: "bg-red-500",
} as const;

const HEALTH_BORDER = {
  healthy: "",
  warning: "",
  critical: "border-red-200 dark:border-red-900 bg-red-50/50 dark:bg-red-950/20",
} as const;

function StackCard({ stack }: { stack: StackSummary }) {
  const health = stackHealth(stack);
  const running = stack.tasksByState["running"] ?? 0;
  const failed = stack.tasksByState["failed"] ?? 0;
  const other = Object.entries(stack.tasksByState)
    .filter(([k]) => k !== "running" && k !== "failed")
    .reduce((sum, [, v]) => sum + v, 0);
  const totalTasks = running + failed + other;

  return (
    <Link
      to={`/stacks/${stack.name}`}
      className={`block rounded-lg border p-4 hover:border-foreground/20 hover:shadow-sm transition-all ${HEALTH_BORDER[health]}`}
    >
      {/* Header */}
      <div className="flex items-center gap-2 mb-3">
        <span className={`shrink-0 w-2.5 h-2.5 rounded-full ${HEALTH_COLORS[health]}`} />
        <span className="font-medium truncate">{stack.name}</span>
        {stack.updatingServices > 0 && (
          <span className="ml-auto text-[10px] font-medium px-1.5 py-0.5 rounded bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
            Updating {stack.updatingServices}
          </span>
        )}
      </div>

      {/* Task bar */}
      <div className="mb-3">
        <div className="flex justify-between text-xs text-muted-foreground mb-1">
          <span>Tasks</span>
          <span className="tabular-nums">
            {running}/{stack.desiredTasks}
          </span>
        </div>
        {totalTasks > 0 ? (
          <div className="h-2 rounded-full bg-muted overflow-hidden flex">
            {running > 0 && (
              <div
                className="bg-green-500 transition-all"
                style={{ width: `${(running / totalTasks) * 100}%` }}
              />
            )}
            {other > 0 && (
              <div
                className="bg-yellow-500 transition-all"
                style={{ width: `${(other / totalTasks) * 100}%` }}
              />
            )}
            {failed > 0 && (
              <div
                className="bg-red-500 transition-all"
                style={{ width: `${(failed / totalTasks) * 100}%` }}
              />
            )}
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
          format={(v) => `${v.toFixed(0)}%`}
        />
      )}

      {/* Resource counts footer */}
      <div className="flex gap-3 mt-3 pt-3 border-t text-[10px] text-muted-foreground">
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
  format: (v: number) => string;
}) {
  const pct = limit > 0 ? Math.min((used / limit) * 100, 100) : 0;
  const color = pct > 90 ? "bg-red-500" : pct > 70 ? "bg-yellow-500" : "bg-blue-500";

  return (
    <div className="mb-2">
      <div className="flex justify-between text-xs text-muted-foreground mb-0.5">
        <span>{label}</span>
        <span className="tabular-nums">
          {format(used)} / {format(limit)}
        </span>
      </div>
      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
        <div
          className={`h-full rounded-full ${color} transition-all`}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}
