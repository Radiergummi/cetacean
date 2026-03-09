import { useParams } from "react-router-dom";
import { useState, useEffect } from "react";
import { api } from "../api/client";
import type { Task } from "../api/types";
import ErrorBoundary from "../components/ErrorBoundary";
import InfoCard from "../components/InfoCard";
import TaskStatusBadge from "../components/TaskStatusBadge";
import LogViewer from "../components/LogViewer";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { timeAgo } from "../components/TimeAgo";

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>();
  const [task, setTask] = useState<Task | null>(null);

  useEffect(() => {
    if (id) {
      api.task(id).then(setTask);
    }
  }, [id]);

  if (!task) return <LoadingDetail />;

  const shortId = task.ID.slice(0, 12);
  const exitCode = task.Status.ContainerStatus?.ExitCode;
  const containerId = task.Status.ContainerStatus?.ContainerID;

  return (
    <div>
      <PageHeader
        title={`Task ${shortId}`}
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: task.ServiceID.slice(0, 12), to: `/services/${task.ServiceID}` },
          { label: shortId },
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
        <InfoCard
          label="Service"
          value={task.ServiceID.slice(0, 12)}
          href={`/services/${task.ServiceID}`}
        />
        <InfoCard label="Node" value={task.NodeID.slice(0, 12)} href={`/nodes/${task.NodeID}`} />
        <InfoCard label="Slot" value={task.Slot ? String(task.Slot) : "\u2014"} />
        <InfoCard label="Image" value={task.Spec.ContainerSpec.Image.split("@")[0]} />
        <InfoCard
          label="Timestamp"
          value={task.Status.Timestamp ? timeAgo(task.Status.Timestamp) : undefined}
        />
        {containerId && <InfoCard label="Container" value={containerId.slice(0, 12)} />}
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
