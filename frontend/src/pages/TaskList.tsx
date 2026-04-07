import { api } from "../api/client";
import type { Task } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { TaskSparkline } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceCard from "../components/ResourceCard";
import ResourceName from "../components/ResourceName";
import TaskStateFilter, { isActiveTask } from "../components/TaskStateFilter";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { useListPage } from "../hooks/useListPage";
import { isCadvisorReady, useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { sortColumn } from "../lib/sortColumn";
import { useCallback, useMemo } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";

export default function TaskList() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const stateFilter = params.get("state");

  const {
    data: tasks,
    loading,
    error,
    retry,
    hasMore,
    loadMore,
    search,
    setSearch,
    sortKey,
    sortDir,
    toggle,
    viewMode,
    setViewMode,
  } = useListPage({
    path: "/tasks",
    sseType: "task",
    defaultSort: "state",
    viewModeKey: "tasks",
    fetchFn: (params, signal) => api.tasks(params, signal),
    keyFn: ({ ID }: Task) => ID,
  });

  const filteredTasks = useMemo(() => {
    if (stateFilter === "__all__") {
      return tasks;
    }

    if (stateFilter) {
      return tasks.filter(({ Status: { State } }) => State === stateFilter);
    }

    return tasks.filter(isActiveTask);
  }, [tasks, stateFilter]);

  const groupedByService = useMemo(() => {
    const groups = new Map<string, { name: string; id: string; tasks: Task[] }>();

    for (const task of filteredTasks) {
      const key = task.ServiceID;
      let group = groups.get(key);

      if (!group) {
        group = { name: task.ServiceName || key.slice(0, 12), id: key, tasks: [] };
        groups.set(key, group);
      }

      group.tasks.push(task);
    }

    return Array.from(groups.values()).sort((a, b) => a.name.localeCompare(b.name));
  }, [filteredTasks]);

  const setStateFilter = useCallback(
    (state: string | null) => {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          if (state) {
            next.set("state", state);
          } else {
            next.delete("state");
          }

          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  const monitoring = useMonitoringStatus();
  const hasCadvisor = isCadvisorReady(monitoring);
  const taskMetrics = useTaskMetrics(`container_label_com_docker_swarm_task_id!=""`, hasCadvisor);

  const columns: Column<Task>[] = useMemo(
    () => [
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
    [sortKey, sortDir, toggle, hasCadvisor, taskMetrics],
  );

  if (loading) {
    return (
      <div>
        <PageHeader title="Tasks" />
        <SkeletonTable columns={hasCadvisor ? 8 : 6} />
      </div>
    );
  }

  if (error) {
    return (
      <FetchError
        message={error.message}
        onRetry={retry}
      />
    );
  }

  return (
    <div>
      <PageHeader title="Tasks" />
      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search tasks…"
        viewMode={viewMode}
        onViewModeChange={setViewMode}
      />
      <div className="mb-4 flex justify-end">
        <TaskStateFilter
          tasks={tasks}
          active={stateFilter}
          onChange={setStateFilter}
        />
      </div>
      {filteredTasks.length === 0 ? (
        <EmptyState
          message={search || stateFilter ? "No tasks match your filters" : "No tasks found"}
        />
      ) : viewMode === "table" ? (
        <DataTable
          columns={columns}
          data={filteredTasks}
          keyFn={({ ID }) => ID}
          onRowClick={({ ID }) => navigate(`/tasks/${ID}`)}
          hasMore={hasMore}
          onLoadMore={loadMore}
        />
      ) : (
        <div className="space-y-6">
          {groupedByService.map((group) => (
            <section key={group.id}>
              <h3 className="mb-2 flex items-center gap-2 text-base font-medium">
                <Link
                  to={`/services/${group.id}`}
                  className="text-link hover:underline"
                >
                  <ResourceName name={group.name} />
                </Link>
                <span className="inline-flex size-5 items-center justify-center rounded-full bg-muted text-xs text-muted-foreground tabular-nums">
                  {group.tasks.length}
                </span>
              </h3>

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
    </div>
  );
}
