import { useParams } from "react-router-dom";
import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { Task } from "../api/types";
import { useSSE } from "../hooks/useSSE";
import ErrorBoundary from "../components/ErrorBoundary";
import InfoCard from "../components/InfoCard";
import TaskStatusBadge from "../components/TaskStatusBadge";
import LogViewer from "../components/LogViewer";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import { ResourceId, ResourceLink, ContainerImage, Timestamp } from "../components/data";

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>();
  const [task, setTask] = useState<Task | null>(null);
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    if (!id) return;
    api.task(id).then(setTask).catch(() => setError(true));
  }, [id]);

  useEffect(fetchData, [fetchData]);

  useSSE(["task"], (e) => {
    if (e.id === id) fetchData();
  });

  if (error) return <FetchError message="Failed to load task" />;
  if (!task) return <LoadingDetail />;

  const shortId = task.ID.slice(0, 7);
  const serviceName = task.ServiceName || task.ServiceID.slice(0, 12);
  const nodeLabel = task.NodeHostname || task.NodeID.slice(0, 12);
  const exitCode = task.Status.ContainerStatus?.ExitCode;
  const containerId = task.Status.ContainerStatus?.ContainerID;

  return (
    <div>
      <PageHeader
        title={`Task ${task.ID}`}
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: serviceName, to: `/services/${task.ServiceID}` },
          { label: task.Slot ? `Replica #${task.Slot} (${shortId})` : shortId },
        ]}
      />

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
        <div className="rounded-lg border bg-card p-4">
          <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1">
            State
          </div>
          <TaskStatusBadge state={task.Status.State} />
        </div>
        <InfoCard label="Desired State" value={task.DesiredState} />
        <ResourceLink label="Service" name={serviceName} to={`/services/${task.ServiceID}`} />
        <ResourceLink label="Node" name={nodeLabel} to={`/nodes/${task.NodeID}`} />
        <InfoCard label="Slot" value={task.Slot ? String(task.Slot) : "\u2014"} />
        <ContainerImage image={task.Spec.ContainerSpec.Image} />
        <Timestamp label="Timestamp" date={task.Status.Timestamp} />
        <ResourceId label="Container" id={containerId} truncate={12} />
        {exitCode != null && exitCode !== 0 && (
          <InfoCard label="Exit Code" value={String(exitCode)} />
        )}
      </div>

      {task.Status.Err && (
        <div className="mb-6 rounded-lg border border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/30 p-4">
          <div className="text-xs font-medium uppercase tracking-wider text-red-600 dark:text-red-400 mb-1">
            Error
          </div>
          <div className="text-sm text-red-700 dark:text-red-300">{task.Status.Err}</div>
        </div>
      )}

      {task.Status.Message && (
        <div className="mb-6 rounded-lg border bg-card p-4">
          <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1">
            Status Message
          </div>
          <div className="text-sm">{task.Status.Message}</div>
        </div>
      )}

      <div className="mb-6">
        <ErrorBoundary inline>
          <LogViewer taskId={id!} header="Logs" />
        </ErrorBoundary>
      </div>
    </div>
  );
}
