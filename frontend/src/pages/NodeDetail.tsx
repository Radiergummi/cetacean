import { useParams, Link } from "react-router-dom";
import { useState, useEffect, useMemo } from "react";
import { api } from "../api/client";
import type { Node, Task, HistoryEntry } from "../api/types";
import ErrorBoundary from "../components/ErrorBoundary";
import MetricsPanel from "../components/MetricsPanel";
import InfoCard from "../components/InfoCard";
import TaskStatusBadge from "../components/TaskStatusBadge";
import TaskStateFilter from "../components/TaskStateFilter";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import NodeResourceGauges from "../components/NodeResourceGauges";
import ActivityFeed from "../components/ActivityFeed";
import { statusColor } from "../lib/statusColor";
import { formatBytes } from "../lib/formatBytes";
import TimeAgo from "../components/TimeAgo";

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>();
  const [node, setNode] = useState<Node | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [stateFilter, setStateFilter] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryEntry[]>([]);

  const [error, setError] = useState(false);

  useEffect(() => {
    if (id) {
      api
        .node(id)
        .then(setNode)
        .catch(() => setError(true));
      api
        .nodeTasks(id)
        .then(setTasks)
        .catch(() => {});
      api
        .history({ resourceId: id, limit: 10 })
        .then(setHistory)
        .catch(() => {});
    }
  }, [id]);

  const filteredTasks = useMemo(() => {
    const filtered = stateFilter ? tasks.filter((t) => t.Status.State === stateFilter) : tasks;
    return [...filtered].sort(
      (a, b) => new Date(b.Status.Timestamp).getTime() - new Date(a.Status.Timestamp).getTime(),
    );
  }, [tasks, stateFilter]);

  if (error) return <FetchError message="Failed to load node" />;
  if (!node) return <LoadingDetail />;

  const addr = node.Status.Addr || "";

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={node.Description.Hostname || node.ID}
        breadcrumbs={[
          { label: "Nodes", to: "/nodes" },
          { label: node.Description.Hostname || node.ID },
        ]}
      />
      <div className="rounded-lg border bg-card p-4">
        <ErrorBoundary inline>
          <NodeResourceGauges instance={addr} />
        </ErrorBoundary>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <InfoCard label="Role" value={node.Spec.Role} />
        <InfoCard label="Status" value={node.Status.State} />
        <InfoCard label="Availability" value={node.Spec.Availability} />
        <InfoCard label="Engine" value={node.Description.Engine.EngineVersion} />
        <InfoCard
          label="OS"
          value={`${node.Description.Platform.OS} ${node.Description.Platform.Architecture}`}
        />
        <InfoCard label="Address" value={addr} />
        <InfoCard
          label="CPUs"
          value={`${(node.Description.Resources.NanoCPUs / 1_000_000_000).toFixed(0)}`}
        />
        <InfoCard label="Memory" value={formatBytes(node.Description.Resources.MemoryBytes)} />
      </div>
      {node.ManagerStatus && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <InfoCard label="Manager" value={node.ManagerStatus.Leader ? "Leader" : "Reachable"} />
          <InfoCard label="Manager Address" value={node.ManagerStatus.Addr} />
        </div>
      )}

      {tasks.length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
              Tasks
            </h2>
            <TaskStateFilter tasks={tasks} active={stateFilter} onChange={setStateFilter} />
          </div>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">ID</th>
                  <th className="text-left p-3 text-sm font-medium">Service</th>
                  <th className="text-left p-3 text-sm font-medium">State</th>
                  <th className="text-left p-3 text-sm font-medium">Desired</th>
                  <th className="text-left p-3 text-sm font-medium">Error</th>
                  <th className="text-left p-3 text-sm font-medium">Timestamp</th>
                </tr>
              </thead>
              <tbody>
                {filteredTasks.map((task) => {
                  const exitCode = task.Status.ContainerStatus?.ExitCode;
                  const errorMsg =
                    task.Status.Err || (exitCode && exitCode !== 0 ? `exit ${exitCode}` : "");
                  return (
                    <tr key={task.ID} className="border-b last:border-b-0">
                      <td className="p-3 text-sm font-mono text-xs">
                        <span className="inline-flex items-center gap-2">
                          <span className={`shrink-0 w-2 h-2 rounded-full ${statusColor(task.Status.State)}`} />
                          <Link to={`/tasks/${task.ID}`} className="text-link hover:underline">
                            {task.ID.slice(0, 12)}
                          </Link>
                        </span>
                      </td>
                      <td className="p-3 text-sm">
                        <Link
                          to={`/services/${task.ServiceID}`}
                          className="text-link hover:underline"
                        >
                          {task.ServiceID.slice(0, 12)}
                        </Link>
                      </td>
                      <td className="p-3 text-sm">
                        <TaskStatusBadge state={task.Status.State} />
                      </td>
                      <td className="p-3 text-sm">{task.DesiredState}</td>
                      <td className="p-3 text-sm text-red-600 dark:text-red-400">{errorMsg}</td>
                      <td className="p-3 text-sm text-muted-foreground">
                        {task.Status.Timestamp ? (
                          <TimeAgo date={task.Status.Timestamp} />
                        ) : (
                          "\u2014"
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {history.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Recent Activity
          </h2>
          <ActivityFeed entries={history} />
        </div>
      )}

      <ErrorBoundary inline>
        <MetricsPanel
          header="Metrics"
          charts={[
            {
              title: "CPU Usage",
              query: `100 - (avg(rate(node_cpu_seconds_total{mode="idle",instance=~"${addr}:.*"}[5m])) * 100)`,
              unit: "%",
            },
            {
              title: "Memory Usage",
              query: `(1 - node_memory_MemAvailable_bytes{instance=~"${addr}:.*"} / node_memory_MemTotal_bytes{instance=~"${addr}:.*"}) * 100`,
              unit: "%",
            },
            {
              title: "Disk I/O",
              query: `sum(rate(node_disk_read_bytes_total{instance=~"${addr}:.*"}[5m]))`,
              unit: "bytes/s",
            },
            {
              title: "Network I/O",
              query: `sum(rate(node_network_receive_bytes_total{device!="lo",instance=~"${addr}:.*"}[5m]))`,
              unit: "bytes/s",
            },
          ]}
        />
      </ErrorBoundary>
    </div>
  );
}
