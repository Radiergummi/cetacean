import { useState, useEffect, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { api, type ClusterSnapshot } from "../api/client";
import { useSSE } from "../hooks/useSSE";
import PageHeader from "../components/PageHeader";

export default function ClusterOverview() {
  const [snapshot, setSnapshot] = useState<ClusterSnapshot | null>(null);

  const fetchSnapshot = useCallback(() => {
    api.cluster().then(setSnapshot);
  }, []);

  useEffect(() => {
    fetchSnapshot();
  }, [fetchSnapshot]);

  useSSE(
    ["node", "service", "task", "stack"],
    useCallback(() => {
      fetchSnapshot();
    }, [fetchSnapshot]),
  );

  if (!snapshot) {
    return (
      <div>
        <PageHeader title="Cluster Overview" />
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div key={i} className="rounded-lg border bg-card p-6">
              <div className="h-4 w-20 bg-muted rounded mb-2" />
              <div className="h-8 w-12 bg-muted rounded" />
            </div>
          ))}
        </div>
      </div>
    );
  }

  const tasksRunning = snapshot.tasksByState?.["running"] || 0;
  const tasksFailed = snapshot.tasksByState?.["failed"] || 0;
  const tasksOther = snapshot.taskCount - tasksRunning - tasksFailed;

  return (
    <div>
      <PageHeader title="Cluster Overview" />
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard label="Nodes Ready" value={snapshot.nodesReady} color="green" to="/nodes" />
        <StatCard
          label="Nodes Down"
          value={snapshot.nodesDown}
          color={snapshot.nodesDown > 0 ? "red" : undefined}
          to="/nodes"
        />
        <StatCard label="Services" value={snapshot.serviceCount} to="/services" />
        <StatCard label="Stacks" value={snapshot.stackCount} to="/stacks" />
        <StatCard label="Tasks Running" value={tasksRunning} color="green" />
        <StatCard
          label="Tasks Failed"
          value={tasksFailed}
          color={tasksFailed > 0 ? "red" : undefined}
        />
        <StatCard label="Tasks Other" value={tasksOther} />
        <StatCard label="Tasks Total" value={snapshot.taskCount} />
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  color,
  to,
}: {
  label: string;
  value: number;
  color?: string;
  to?: string;
}) {
  const navigate = useNavigate();
  const valueColor = color === "green" ? "text-green-600" : color === "red" ? "text-red-600" : "";
  const bgTint =
    color === "red" && value > 0
      ? "bg-red-50 dark:bg-red-950/30 border-red-200 dark:border-red-900"
      : "bg-card";

  return (
    <div
      className={`rounded-lg border p-5 ${bgTint} ${to ? "cursor-pointer hover:border-foreground/20 hover:shadow-sm transition-all" : ""}`}
      onClick={to ? () => navigate(to) : undefined}
    >
      <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1.5">
        {label}
      </div>
      <div className={`text-3xl font-semibold tabular-nums ${valueColor}`}>{value}</div>
    </div>
  );
}
