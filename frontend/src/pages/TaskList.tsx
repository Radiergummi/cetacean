import { useCallback } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useSortParams } from "../hooks/useSort";
import { useSearchParam } from "../hooks/useSearchParam";
import { api } from "../api/client";
import type { Task } from "../api/types";

type TaskListItem = Task & { ServiceName?: string; NodeHostname?: string };
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import DataTable, { type Column } from "../components/DataTable";
import SortIndicator from "../components/SortIndicator";
import TaskStatusBadge from "../components/TaskStatusBadge";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { SkeletonTable } from "../components/LoadingSkeleton";

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
    (t: TaskListItem) => t.ID,
  );
  const columns: Column<TaskListItem>[] = [
    {
      header: "Service",
      cell: (t) => (
        <Link
          to={`/services/${t.ServiceID}`}
          className="text-link hover:underline font-medium"
          onClick={(e) => e.stopPropagation()}
        >
          {t.ServiceName || t.ServiceID.slice(0, 12)}
        </Link>
      ),
    },
    {
      header: <SortIndicator label="State" active={sortKey === "state"} dir={sortDir} />,
      cell: (t) => <TaskStatusBadge state={t.Status.State} />,
      onHeaderClick: () => toggle("state"),
    },
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
        <SkeletonTable columns={6} />
      </div>
    );
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  return (
    <div>
      <PageHeader title="Tasks" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search tasks..." />
      </div>
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
