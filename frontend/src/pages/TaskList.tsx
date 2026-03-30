import { api } from "../api/client";
import type { Task } from "../api/types";
import DataTable, { type Column } from "../components/DataTable";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import { SkeletonTable } from "../components/LoadingSkeleton";
import { TaskSparkline } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import SortIndicator from "../components/SortIndicator";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useSearchParam } from "../hooks/useSearchParam";
import { useSortParams } from "../hooks/useSort";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { useCallback, useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";

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
    ({ ID }: Task) => ID,
  );

  const monitoring = useMonitoringStatus();
  const hasCadvisor = !!monitoring?.cadvisor?.targets;
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
        header: (
          <SortIndicator
            label="State"
            active={sortKey === "state"}
            dir={sortDir}
          />
        ),
        cell: ({ Status }) => <TaskStatusBadge state={Status.State} />,
        onHeaderClick: () => toggle("state"),
      },
      ...(hasCadvisor
        ? [
            {
              header: "CPU",
              cell: ({ ID, Status }: Task) =>
                Status.State === "running" ? (
                  <TaskSparkline
                    data={taskMetrics.get(ID)?.cpu}
                    currentValue={taskMetrics.get(ID)?.currentCpu}
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
                    data={taskMetrics.get(ID)?.memory}
                    currentValue={taskMetrics.get(ID)?.currentMemory}
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
        header: (
          <SortIndicator
            label="Node"
            active={sortKey === "node"}
            dir={sortDir}
          />
        ),
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
        onHeaderClick: () => toggle("node"),
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
      />
      {tasks.length === 0 ? (
        <EmptyState message={search ? "No tasks match your search" : "No tasks found"} />
      ) : (
        <DataTable
          columns={columns}
          data={tasks}
          keyFn={({ ID }) => ID}
          onRowClick={({ ID }) => navigate(`/tasks/${ID}`)}
        />
      )}
    </div>
  );
}
