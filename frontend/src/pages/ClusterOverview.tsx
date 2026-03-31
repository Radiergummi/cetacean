import { api, type ClusterSnapshot } from "../api/client";
import type { HistoryEntry } from "../api/types";
import ActivityFeed from "../components/ActivityFeed";
import CollapsibleSection from "../components/CollapsibleSection";
import {
  CapacitySection,
  MetricsPanel,
  MonitoringStatus,
  StackDrillDownChart,
} from "../components/metrics";
import PageHeader from "../components/PageHeader";
import RecommendationSummary from "../components/RecommendationSummary";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useResourceStream } from "../hooks/useResourceStream";
import { TrendingDown, TrendingUp } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";

export default function ClusterOverview() {
  const [snapshot, setSnapshot] = useState<ClusterSnapshot | null>(null);
  const prevRef = useRef<ClusterSnapshot | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const fetchSnapshot = useCallback(() => {
    api
      .cluster()
      .then((snapshot) => {
        setSnapshot((previous) => {
          if (previous) {
            prevRef.current = previous;
          }

          return snapshot;
        });
      })
      .catch(console.warn);
  }, []);

  const fetchHistory = useCallback(() => {
    api
      .history({ limit: 25 })
      .then((entry) => {
        setHistory(entry);
        setHistoryLoading(false);
      })
      .catch(() => {
        setHistoryLoading(false);
      });
  }, []);

  useEffect(() => {
    fetchSnapshot();
    fetchHistory();

    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, [fetchSnapshot, fetchHistory]);

  useResourceStream(
    "/events",
    useCallback(() => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        fetchSnapshot();
        fetchHistory();
      }, 2_000);
    }, [fetchSnapshot, fetchHistory]),
  );

  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;

  if (!snapshot) {
    return (
      <div>
        <PageHeader title="Cluster Overview" />
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          {Array.from({ length: 4 }).map((_, index) => (
            <div
              key={index}
              className="rounded-lg border bg-card p-6"
            >
              <div className="mb-2 h-4 w-20 rounded bg-muted" />
              <div className="h-8 w-12 rounded bg-muted" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  const prev = prevRef.current;
  const tasksFailed = snapshot.tasksByState?.["failed"] || 0;
  const prevFailed = prev?.tasksByState?.["failed"] || 0;
  const tasksRunning = snapshot.tasksByState?.["running"] || 0;

  return (
    <div>
      <PageHeader title="Cluster Overview" />

      {monitoring && <MonitoringStatus status={monitoring} />}

      {/* Health Row */}
      <div className="mb-6 grid grid-cols-2 gap-4 md:grid-cols-4">
        <HealthCard
          label="Nodes"
          primary={`${snapshot.nodesReady}/${snapshot.nodeCount} ready`}
          secondary={
            [
              snapshot.nodesDown > 0 && `${snapshot.nodesDown} down`,
              snapshot.nodesDraining > 0 && `${snapshot.nodesDraining} draining`,
            ]
              .filter(Boolean)
              .join(", ") || "all ready"
          }
          status={snapshot.nodesDown > 0 ? "red" : snapshot.nodesDraining > 0 ? "amber" : "green"}
          to="/nodes"
        />
        <HealthCard
          label="Services"
          primary={`${snapshot.servicesConverged}/${snapshot.serviceCount} converged`}
          secondary={
            snapshot.servicesDegraded > 0 ? `${snapshot.servicesDegraded} degraded` : "all healthy"
          }
          status={snapshot.servicesDegraded > 0 ? "amber" : "green"}
          to="/services"
        />
        <HealthCard
          label="Failed Tasks"
          primary={String(tasksFailed)}
          secondary={tasksFailed > 0 ? "needs attention" : "none"}
          status={tasksFailed > 0 ? "red" : "neutral"}
          delta={prev ? tasksFailed - prevFailed : undefined}
          to={tasksFailed > 0 ? "/tasks?state=failed" : "/tasks"}
        />
        <HealthCard
          label="Tasks"
          primary={`${tasksRunning} running`}
          secondary={`${snapshot.taskCount} total · ${snapshot.stackCount} stacks`}
          status="neutral"
          to="/tasks"
        />
      </div>

      {/* Two-column: Capacity + Activity */}
      <div className="mb-6 grid grid-cols-1 gap-6 md:grid-cols-2">
        <CollapsibleSection title="Capacity">
          <CapacitySection snapshot={snapshot} />
          <div className="mt-4">
            <RecommendationSummary />
          </div>
        </CollapsibleSection>
        <CollapsibleSection title="Recent Activity">
          <div className="max-h-80 overflow-y-auto rounded-lg border bg-card p-4">
            <ActivityFeed
              entries={history}
              loading={historyLoading}
            />
          </div>
        </CollapsibleSection>
      </div>

      {hasPrometheus && (
        <div className="mb-6">
          <MetricsPanel
            header="Resource Usage by Stack"
            stackable
          >
            <StackDrillDownChart
              title="CPU Usage (by Stack)"
              stackQuery={`topk(10, sum by (container_label_com_docker_stack_namespace)(rate(container_cpu_usage_seconds_total{container_label_com_docker_stack_namespace!=""}[5m])) * 100)`}
              serviceQueryTemplate={`sum by (container_label_com_docker_swarm_service_name)(rate(container_cpu_usage_seconds_total{container_label_com_docker_stack_namespace="<STACK>", container_label_com_docker_swarm_service_name!=""}[5m])) * 100`}
              unit="%"
              yMin={0}
              stackable
            />
            <StackDrillDownChart
              title="Memory Usage (by Stack)"
              stackQuery={`topk(10, sum by (container_label_com_docker_stack_namespace)(container_memory_usage_bytes{container_label_com_docker_stack_namespace!=""}))`}
              serviceQueryTemplate={`sum by (container_label_com_docker_swarm_service_name)(container_memory_usage_bytes{container_label_com_docker_stack_namespace="<STACK>", container_label_com_docker_swarm_service_name!=""})`}
              unit="bytes"
              yMin={0}
              stackable
            />
          </MetricsPanel>
        </div>
      )}
    </div>
  );
}

function HealthCard({
  label,
  primary,
  secondary,
  status,
  delta,
  to,
}: {
  label: string;
  primary: string;
  secondary: string;
  status: "green" | "amber" | "red" | "neutral";
  delta?: number;
  to: string;
}) {
  return (
    <Link
      to={to}
      data-status={status}
      className="group block cursor-pointer rounded-lg border bg-card p-5 transition-all hover:border-foreground/20 hover:shadow-sm data-[status=amber]:border-amber-500/30 data-[status=amber]:bg-amber-500/5 data-[status=green]:border-green-500/30 data-[status=green]:bg-green-500/5 data-[status=red]:border-red-500/30 data-[status=red]:bg-red-500/5"
    >
      <div className="mb-1.5 text-xs font-medium tracking-wider text-muted-foreground uppercase">
        {label}
      </div>
      <div className="flex items-center gap-2">
        <span className="text-2xl font-semibold tabular-nums group-data-[status=amber]:text-amber-500 group-data-[status=green]:text-green-500 group-data-[status=red]:text-red-500">
          {primary}
        </span>
        {delta != null && delta !== 0 && (
          <span
            data-negative={delta < 0 || undefined}
            className="flex items-center gap-0.5 text-xs font-medium text-red-500 data-negative:text-green-500"
          >
            {delta > 0 ? <TrendingUp className="size-3" /> : <TrendingDown className="size-3" />}
            {delta > 0 ? `+${delta}` : delta}
          </span>
        )}
      </div>
      <div className="mt-1 text-xs text-muted-foreground">{secondary}</div>
    </Link>
  );
}
