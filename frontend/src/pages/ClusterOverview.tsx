import { useState, useEffect, useCallback, useRef } from "react";
import { Link } from "react-router-dom";
import { TrendingUp, TrendingDown } from "lucide-react";
import { api, type ClusterSnapshot } from "../api/client";
import type { HistoryEntry } from "../api/types";
import { useResourceStream } from "../hooks/useResourceStream";
import PageHeader from "../components/PageHeader";
import ActivityFeed from "../components/ActivityFeed";
import CollapsibleSection from "../components/CollapsibleSection";
import DiskUsageSection from "../components/DiskUsageSection";
import {
  MetricsPanel,
  MonitoringStatus,
  CapacitySection,
  StackDrillDownChart,
} from "../components/metrics";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";

export default function ClusterOverview() {
  const [snapshot, setSnapshot] = useState<ClusterSnapshot | null>(null);
  const prevRef = useRef<ClusterSnapshot | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const fetchSnapshot = useCallback(() => {
    api
      .cluster()
      .then((s) => {
        setSnapshot((prev) => {
          if (prev) prevRef.current = prev;
          return s;
        });
      })
      .catch(() => {});
  }, []);

  const fetchHistory = useCallback(() => {
    api
      .history({ limit: 25 })
      .then((h) => {
        setHistory(h);
        setHistoryLoading(false);
      })
      .catch(() => {
        setHistoryLoading(false);
      });
  }, []);

  useEffect(() => {
    fetchSnapshot();
    fetchHistory();
  }, [fetchSnapshot, fetchHistory]);

  useResourceStream(
    "/events",
    useCallback(() => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        fetchSnapshot();
        fetchHistory();
      }, 2000);
    }, [fetchSnapshot, fetchHistory]),
  );

  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;

  if (!snapshot) {
    return (
      <div>
        <PageHeader title="Cluster Overview" />
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-lg border bg-card p-6">
              <div className="h-4 w-20 bg-muted rounded mb-2" />
              <div className="h-8 w-12 bg-muted rounded" />
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
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
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
          to="/tasks"
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
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
        <CollapsibleSection title="Capacity">
          <CapacitySection snapshot={snapshot} />
        </CollapsibleSection>
        <CollapsibleSection title="Recent Activity">
          <div className="max-h-80 overflow-y-auto rounded-lg border bg-card p-4">
            <ActivityFeed entries={history} loading={historyLoading} />
          </div>
        </CollapsibleSection>
      </div>

      {hasPrometheus && (
        <div className="mb-6">
          <MetricsPanel header="Resource Usage by Stack" stackable>
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

      <DiskUsageSection />
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
  const borderColor = {
    green: "border-green-500/30",
    amber: "border-amber-500/30",
    red: "border-red-500/30",
    neutral: "",
  }[status];

  const bgTint = {
    green: "bg-green-500/5",
    amber: "bg-amber-500/5",
    red: "bg-red-500/5",
    neutral: "bg-card",
  }[status];

  const primaryColor = {
    green: "text-green-500",
    amber: "text-amber-500",
    red: "text-red-500",
    neutral: "",
  }[status];

  return (
    <Link
      to={to}
      className={`block rounded-lg border p-5 cursor-pointer hover:border-foreground/20 hover:shadow-sm transition-all ${borderColor} ${bgTint}`}
    >
      <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1.5">
        {label}
      </div>
      <div className="flex items-center gap-2">
        <span className={`text-2xl font-semibold tabular-nums ${primaryColor}`}>{primary}</span>
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
      <div className="text-xs text-muted-foreground mt-1">{secondary}</div>
    </Link>
  );
}
