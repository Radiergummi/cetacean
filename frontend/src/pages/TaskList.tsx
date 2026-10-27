import { api } from "../api/client";
import type { Task } from "../api/types";
import type { Column } from "../components/DataTable";
import { TaskSparkline } from "../components/metrics";
import ResourceCard from "../components/ResourceCard";
import ResourceListPage from "../components/ResourceListPage";
import ResourceName from "../components/ResourceName";
import TaskStateFilter, { taskStateExpression } from "../components/TaskStateFilter";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { isCadvisorReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import type { SortDir } from "../hooks/useSort";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { useUrlParam } from "../hooks/useUrlParam";
import { sortColumn } from "../lib/sortColumn";
import { useCallback } from "react";
import { Link } from "react-router-dom";

export default function TaskList() {
  const [stateFilter, setStateFilter] = useUrlParam("state");

  const monitoring = useMonitoringStatus();
  const hasCadvisor = isCadvisorReady(monitoring);
  const taskMetrics = useTaskMetrics(`container_label_com_docker_swarm_task_id!=""`, hasCadvisor);

  const columns = useCallback(
    (
      sortKey: string | undefined,
      sortDir: SortDir,
      toggle: (key: string) => void,
    ): Column<Task>[] => [
      {
        header: "Service",
        cell: ({ ServiceID, ServiceName }) => (
          <Link
            to={`/services/${ServiceID}`}
            className="font-medium text-link hover:underline"
            onClick={(event) => event.stopPropagation()}
          >
            <ResourceName name={ServiceName || ServiceID.slice(0, 12)} />
          </Link>
        ),
      },
      {
        ...sortColumn("State", "state", sortKey, sortDir, toggle),
        cell: ({ Status }) => <TaskStatusBadge state={Status.State} />,
      },
      ...(hasCadvisor
        ? [
            {
              header: "CPU",
              cell: ({ ID, Status }: Task) =>
                Status.State === "running" ? (
                  <TaskSparkline
                    data={taskMetrics[ID]?.cpu}
                    currentValue={taskMetrics[ID]?.currentCpu}
                    type="cpu"
                  />
                ) : (
                  "\u2014"
                ),
            },
            {
              header: "Memory",
              cell: ({ ID, Status }: Task) =>
                Status.State === "running" ? (
                  <TaskSparkline
                    data={taskMetrics[ID]?.memory}
                    currentValue={taskMetrics[ID]?.currentMemory}
                    type="memory"
                  />
                ) : (
                  "\u2014"
                ),
            },
          ]
        : []),
      {
        header: "Desired",
        cell: ({ DesiredState }) => DesiredState,
      },
      {
        ...sortColumn("Node", "node", sortKey, sortDir, toggle),
        cell: ({ NodeHostname, NodeID }) =>
          NodeID ? (
            <Link
              to={`/nodes/${NodeID}`}
              className="text-link hover:underline"
              onClick={(event) => event.stopPropagation()}
            >
              {NodeHostname || NodeID.slice(0, 12)}
            </Link>
          ) : (
            "\u2014"
          ),
      },
      {
        header: "Slot",
        cell: ({ Slot }) => (Slot ? String(Slot) : "\u2014"),
      },
      {
        header: "Image",
        cell: ({ Spec }) => (
          <span className="font-mono text-xs">{Spec.ContainerSpec?.Image?.split("@")[0]}</span>
        ),
      },
    ],
    [hasCadvisor, taskMetrics],
  );

  return (
    <ResourceListPage<Task>
      title="Tasks"
      path="/tasks"
      sseType="task"
      defaultSort="state"
      searchPlaceholder="Search tasks…"
      viewModeKey="tasks"
      fetchFn={(params, signal) => api.tasks(params, signal)}
      keyFn={({ ID }) => ID}
      itemPath={({ ID }) => `/tasks/${ID}`}
      filter={taskStateExpression(stateFilter)}
      columns={columns}
      headerContent={(tasks, total) => (
        <div className="mb-4 flex justify-end">
          <TaskStateFilter
            tasks={tasks}
            total={total}
            active={stateFilter}
            onChange={setStateFilter}
          />
        </div>
      )}
      emptyMessage={(hasSearch) =>
        hasSearch || stateFilter ? "No tasks match your filters" : "No tasks found"
      }
      renderGrid={(tasks) => (
        <div className="space-y-6">
          {groupByService(tasks).map((group) => (
            <section key={group.id}>
              <h2 className="mb-2 flex items-center gap-2 text-base font-medium">
                <Link
                  to={`/services/${group.id}`}
                  className="text-link hover:underline"
                >
                  <ResourceName name={group.name} />
                </Link>
                <span className="inline-flex size-5 items-center justify-center rounded-full bg-muted text-xs text-muted-foreground tabular-nums">
                  {group.tasks.length}
                </span>
              </h2>

              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {group.tasks.map((task) => (
                  <ResourceCard
                    key={task.ID}
                    title={task.Slot ? `Slot ${task.Slot}` : task.ID.slice(0, 12)}
                    to={`/tasks/${task.ID}`}
                    badge={<TaskStatusBadge state={task.Status.State} />}
                    subtitle={task.NodeHostname}
                    meta={[<span key="desired">{task.DesiredState}</span>]}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    />
  );
}

function groupByService(tasks: Task[]) {
  const groups = new Map<string, { name: string; id: string; tasks: Task[] }>();

  for (const task of tasks) {
    const key = task.ServiceID;
    let group = groups.get(key);

    if (!group) {
      group = { name: task.ServiceName || key.slice(0, 12), id: key, tasks: [] };
      groups.set(key, group);
    }

    group.tasks.push(task);
  }

  return Array.from(groups.values()).sort((a, b) => a.name.localeCompare(b.name));
}
