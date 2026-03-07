import { useParams, Link } from "react-router-dom";
import { useState, useEffect, useMemo } from "react";
import { api } from "../api/client";
import type { Node, Task } from "../api/types";
import MetricsPanel from "../components/MetricsPanel";
import InfoCard from "../components/InfoCard";
import TaskStatusBadge from "../components/TaskStatusBadge";
import TaskStateFilter from "../components/TaskStateFilter";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import NodeResourceGauges from "../components/NodeResourceGauges";

export default function NodeDetail() {
  const { id } = useParams<{ id: string }>();
  const [node, setNode] = useState<Node | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [stateFilter, setStateFilter] = useState<string | null>(null);

  useEffect(() => {
    if (id) {
      api.node(id).then(setNode);
      api
        .nodeTasks(id)
        .then(setTasks)
        .catch(() => {});
    }
  }, [id]);

  const filteredTasks = useMemo(() => {
    const filtered = stateFilter ? tasks.filter((t) => t.Status.State === stateFilter) : tasks;
    return [...filtered].sort(
      (a, b) => new Date(b.Status.Timestamp).getTime() - new Date(a.Status.Timestamp).getTime(),
    );
  }, [tasks, stateFilter]);

  if (!node) return <LoadingDetail />;

  const addr = node.Status.Addr || "";

  return (
    <div>
      <PageHeader
        title={node.Description.Hostname || node.ID}
        breadcrumbs={[
          { label: "Nodes", to: "/nodes" },
          { label: node.Description.Hostname || node.ID },
        ]}
      />
      <div className="rounded-lg border bg-card p-4 mb-6">
        <NodeResourceGauges />
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <InfoCard label="Role" value={node.Spec.Role} />
        <InfoCard label="Status" value={node.Status.State} />
        <InfoCard label="Availability" value={node.Spec.Availability} />
        <InfoCard label="Engine" value={node.Description.Engine.EngineVersion} />
        <InfoCard
          label="OS"
          value={`${node.Description.Platform.OS} ${node.Description.Platform.Architecture}`}
        />
        <InfoCard label="Address" value={addr} />
      </div>

      {tasks.length > 0 && (
        <div className="mb-6">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
              Tasks
            </h2>
            <TaskStateFilter tasks={tasks} active={stateFilter} onChange={setStateFilter} />
          </div>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead>
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
                  const isFailed =
                    task.Status.State === "failed" || task.Status.State === "rejected";
                  const exitCode = task.Status.ContainerStatus?.ExitCode;
                  const errorMsg =
                    task.Status.Err || (exitCode && exitCode !== 0 ? `exit ${exitCode}` : "");
                  return (
                    <tr key={task.ID} className={`border-b ${isFailed ? "bg-red-50" : ""}`}>
                      <td className="p-3 text-sm font-mono text-xs">
                        <Link to={`/tasks/${task.ID}`} className="text-link hover:underline">
                          {task.ID.slice(0, 12)}
                        </Link>
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
                      <td className="p-3 text-sm text-red-600">{errorMsg}</td>
                      <td className="p-3 text-sm text-muted-foreground">
                        {task.Status.Timestamp
                          ? new Date(task.Status.Timestamp).toLocaleString()
                          : "\u2014"}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <MetricsPanel
        charts={[
          {
            title: "CPU Usage",
            query: `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`,
            unit: "%",
          },
          {
            title: "Memory Usage",
            query: `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100`,
            unit: "%",
          },
          {
            title: "Disk I/O",
            query: `sum(rate(node_disk_read_bytes_total[5m]))`,
            unit: "bytes/s",
          },
          {
            title: "Network I/O",
            query: `sum(rate(node_network_receive_bytes_total{device!="lo"}[5m]))`,
            unit: "bytes/s",
          },
        ]}
      />
    </div>
  );
}
