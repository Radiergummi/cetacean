import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useSortParams } from "../hooks/useSort";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Task } from "../api/types";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import DataTable, { type Column } from "../components/DataTable";
import SortIndicator from "../components/SortIndicator";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { TaskSparkline } from "../components/metrics";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";
import ResourceName from "../components/ResourceName";

export default function TaskList() {
  const navigate = useNavigate();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { sortKey, sortDir, toggle } = useSortParams("state");
  const {
    data: tasks,
    loading,
    error,
    retry,
  } = useSwarmResource(
    useCallback(
      () => api.tasks({ search: debouncedSearch, sort: sortKey, dir: sortDir }),
      [debouncedSearch, sortKey, sortDir],
    ),
    "task",
    (t: Task) => t.ID,
  );

  const monitoring = useMonitoringStatus();
  const hasCadvisor = !!monitoring?.cadvisor?.targets;
  const taskMetrics = useTaskMetrics(`container_label_com_docker_swarm_task_id!=""`, hasCadvisor);

  const columns: Column<Task>[] = [
    {
      header: "Service",
      cell: (t) => (
        <Link
          to={`/services/${t.ServiceID}`}
          className="text-link hover:underline font-medium"
          onClick={(e) => e.stopPropagation()}
        >
          <ResourceName name={t.ServiceName || t.ServiceID.slice(0, 12)} />
        </Link>
      ),
    },
    {
      header: <SortIndicator label="State" active={sortKey === "state"} dir={sortDir} />,
      cell: (t) => <TaskStatusBadge state={t.Status.State} />,
      onHeaderClick: () => toggle("state"),
    },
    ...(hasCadvisor
      ? [
          {
            header: "CPU",
            cell: (t: Task) =>
              t.Status.State === "running" ? (
                <TaskSparkline
                  data={taskMetrics.get(t.ID)?.cpu}
                  currentValue={taskMetrics.get(t.ID)?.currentCpu}
                  type="cpu"
                />
              ) : (
                "\u2014"
              ),
          },
          {
            header: "Memory",
            cell: (t: Task) =>
              t.Status.State === "running" ? (
                <TaskSparkline
                  data={taskMetrics.get(t.ID)?.memory}
                  currentValue={taskMetrics.get(t.ID)?.currentMemory}
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
      cell: (t) => t.DesiredState,
    },
    {
      header: <SortIndicator label="Node" active={sortKey === "node"} dir={sortDir} />,
      cell: (t) =>
        t.NodeID ? (
          <Link
            to={`/nodes/${t.NodeID}`}
            className="text-link hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            {t.NodeHostname || t.NodeID.slice(0, 12)}
          </Link>
        ) : (
          "\u2014"
        ),
      onHeaderClick: () => toggle("node"),
    },
    {
      header: "Slot",
      cell: (t) => (t.Slot ? String(t.Slot) : "\u2014"),
    },
    {
      header: "Image",
      cell: (t) => (
        <span className="font-mono text-xs">{t.Spec.ContainerSpec.Image.split("@")[0]}</span>
      ),
    },
  ];

  if (loading)
    return (
      <div>
        <PageHeader title="Tasks" />
        <SkeletonTable columns={hasCadvisor ? 8 : 6} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Tasks" />
      <ListToolbar search={search} onSearchChange={setSearch} placeholder="Search tasks..." />
      {tasks.length === 0 ? (
        <EmptyState message={search ? "No tasks match your search" : "No tasks found"} />
      ) : (
        <DataTable
          columns={columns}
          data={tasks}
          keyFn={(t) => t.ID}
          onRowClick={(t) => navigate(`/tasks/${t.ID}`)}
        />
      )}
    </div>
  );
}
