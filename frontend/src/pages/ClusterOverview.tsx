import { useState, useEffect, useCallback, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { TrendingUp, TrendingDown } from "lucide-react";
import { api, type ClusterSnapshot } from "../api/client";
import type { HistoryEntry, NotificationRuleStatus } from "../api/types";
import { useSSE } from "../hooks/useSSE";
import PageHeader from "../components/PageHeader";
import ActivityFeed from "../components/ActivityFeed";
import NotificationRules from "../components/NotificationRules";

export default function ClusterOverview() {
  const [snapshot, setSnapshot] = useState<ClusterSnapshot | null>(null);
  const prevRef = useRef<ClusterSnapshot | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(true);
  const [notifRules, setNotifRules] = useState<NotificationRuleStatus[]>([]);

  const fetchSnapshot = useCallback(() => {
    api.cluster().then((s) => {
      setSnapshot((prev) => {
        if (prev) prevRef.current = prev;
        return s;
      });
    });
  }, []);

  useEffect(() => {
    fetchSnapshot();
    api
      .history({ limit: 20 })
      .then(setHistory)
      .catch(() => {})
      .finally(() => setHistoryLoading(false));
    api
      .notificationRules()
      .then(setNotifRules)
      .catch(() => {});
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
          {Array.from({ length: 10 }).map((_, i) => (
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
  const tasksRunning = snapshot.tasksByState?.["running"] || 0;
  const tasksFailed = snapshot.tasksByState?.["failed"] || 0;
  const tasksOther = snapshot.taskCount - tasksRunning - tasksFailed;
  const prevRunning = prev?.tasksByState?.["running"] || 0;
  const prevFailed = prev?.tasksByState?.["failed"] || 0;
  const prevOther = prev ? prev.taskCount - prevRunning - prevFailed : 0;

  return (
    <div>
      <PageHeader title="Cluster Overview" />
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard
          label="Nodes Ready"
          value={snapshot.nodesReady}
          prev={prev?.nodesReady}
          color="green"
          to="/nodes"
        />
        <StatCard
          label="Nodes Down"
          value={snapshot.nodesDown}
          prev={prev?.nodesDown}
          color={snapshot.nodesDown > 0 ? "red" : undefined}
          to="/nodes"
        />
        <StatCard
          label="Services"
          value={snapshot.serviceCount}
          prev={prev?.serviceCount}
          to="/services"
        />
        <StatCard label="Stacks" value={snapshot.stackCount} prev={prev?.stackCount} to="/stacks" />
        <StatCard
          label="Tasks Running"
          value={tasksRunning}
          prev={prev ? prevRunning : undefined}
          color="green"
        />
        <StatCard
          label="Tasks Failed"
          value={tasksFailed}
          prev={prev ? prevFailed : undefined}
          color={tasksFailed > 0 ? "red" : undefined}
        />
        <StatCard label="Tasks Other" value={tasksOther} prev={prev ? prevOther : undefined} />
        <StatCard label="Tasks Total" value={snapshot.taskCount} prev={prev?.taskCount} />
        <StatCard
          label="Total CPU"
          value={snapshot.totalCPU ?? 0}
          formatted={(snapshot.totalCPU ?? 0) + " cores"}
        />
        <StatCard
          label="Total Memory"
          value={snapshot.totalMemory ?? 0}
          formatted={((snapshot.totalMemory ?? 0) / 1024 ** 3).toFixed(1) + " GB"}
        />
      </div>

      <div className="mt-6">
        <h2 className="text-lg font-semibold mb-3">Recent Activity</h2>
        <ActivityFeed entries={history} loading={historyLoading} />
      </div>

      {notifRules.length > 0 && (
        <div className="mt-6">
          <h2 className="text-lg font-semibold mb-3">Notification Rules</h2>
          <NotificationRules rules={notifRules} />
        </div>
      )}
    </div>
  );
}

function StatCard({
  label,
  value,
  prev,
  color,
  to,
  formatted,
}: {
  label: string;
  value: number;
  prev?: number;
  color?: string;
  to?: string;
  formatted?: string;
}) {
  const navigate = useNavigate();
  const valueColor = color === "green" ? "text-green-600" : color === "red" ? "text-red-600" : "";
  const bgTint =
    color === "red" && value > 0
      ? "bg-red-50 dark:bg-red-950/30 border-red-200 dark:border-red-900"
      : "bg-card";
  const delta = prev != null ? value - prev : 0;

  return (
    <div
      className={`rounded-lg border p-5 ${bgTint} ${to ? "cursor-pointer hover:border-foreground/20 hover:shadow-sm transition-all" : ""}`}
      onClick={to ? () => navigate(to) : undefined}
    >
      <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1.5">
        {label}
      </div>
      <div className="flex items-center gap-2">
        <span className={`text-3xl font-semibold tabular-nums ${valueColor}`}>
          {formatted ?? value}
        </span>
        {delta !== 0 && (
          <span
            className={`flex items-center gap-0.5 text-xs font-medium ${delta > 0 ? "text-green-600" : "text-red-600"}`}
          >
            {delta > 0 ? <TrendingUp className="w-3 h-3" /> : <TrendingDown className="w-3 h-3" />}
            {delta > 0 ? `+${delta}` : delta}
          </span>
        )}
      </div>
    </div>
  );
}
