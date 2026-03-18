import { api } from "../api/client";
import type { Service, Task } from "../api/types";
import { ContainerImage, ResourceId, ResourceLink, Timestamp } from "../components/data";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { LogViewer } from "../components/log";
import { MetricsPanel, ResourceGauge } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import { Spinner } from "../components/Spinner";
import TaskStatusBadge from "../components/TaskStatusBadge";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { formatBytes, formatPercentage } from "../lib/format";
import { escapePromQL } from "../lib/utils";
import { Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [task, setTask] = useState<Task | null>(null);
  const [service, setService] = useState<Service | null>(null);
  const [error, setError] = useState(false);
  const [removeLoading, setRemoveLoading] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  const fetchData = useCallback(() => {
    if (!id) {
      return;
    }
    api
      .task(id)
      .then((t) => {
        setTask(t);
        api
          .service(t.ServiceID)
          .then((r) => setService(r.service))
          .catch(() => {});
      })
      .catch(() => setError(true));
  }, [id]);

  useEffect(fetchData, [fetchData]);

  useResourceStream(`/tasks/${id}`, fetchData);

  async function handleRemove() {
    if (!id) {
      return;
    }
    if (
      !window.confirm(
        "Are you sure you want to force-remove this task? This will kill the backing container.",
      )
    ) {
      return;
    }
    setRemoveLoading(true);
    setRemoveError(null);
    try {
      await api.removeTask(id);
      navigate(task?.ServiceID ? `/services/${task.ServiceID}` : "/tasks");
    } catch (err) {
      setRemoveError(err instanceof Error ? err.message : "Failed to remove task");
      setRemoveLoading(false);
    }
  }

  const monitoring = useMonitoringStatus();
  const hasCadvisor = !!monitoring?.cadvisor?.targets;
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const taskMetrics = useTaskMetrics(
    id ? `container_label_com_docker_swarm_task_id="${escapePromQL(id)}"` : "",
    hasCadvisor && !!id && task?.Status.State === "running",
  );
  const myMetrics = id ? taskMetrics.get(id) : undefined;

  if (error) {
    return <FetchError message="Failed to load task" />;
  }
  if (!task) {
    return <LoadingDetail />;
  }

  const serviceName = task.ServiceName || task.ServiceID.slice(0, 12);
  const nodeLabel = task.NodeHostname || task.NodeID.slice(0, 12);
  const taskLabel = task.Slot
    ? `${serviceName} Replica #${task.Slot}`
    : `Task ${task.ID.slice(0, 12)}`;
  const exitCode = task.Status.ContainerStatus?.ExitCode;
  const containerId = task.Status.ContainerStatus?.ContainerID;

  return (
    <div>
      <PageHeader
        title={taskLabel}
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: serviceName, to: `/services/${task.ServiceID}` },
          { label: task.Slot ? `Replica #${task.Slot}` : task.ID.slice(0, 12) },
        ]}
        actions={
          <>
            <button
              type="button"
              onClick={() => void handleRemove()}
              disabled={removeLoading}
              className="inline-flex items-center gap-1.5 rounded border border-red-300 px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950/30"
            >
              {removeLoading ? <Spinner className="size-3" /> : <Trash2 className="size-3.5" />}
              Force Remove
            </button>
            {removeError && <p className="text-xs text-red-600 dark:text-red-400">{removeError}</p>}
          </>
        }
      />

      <div className="mb-6 grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        <div className="rounded-lg border bg-card p-4">
          <div className="mb-1 text-xs font-medium tracking-wider text-muted-foreground uppercase">
            State
          </div>
          <TaskStatusBadge state={task.Status.State} />
        </div>
        <InfoCard
          label="Desired State"
          value={task.DesiredState}
        />
        <ResourceLink
          label="Service"
          name={serviceName}
          to={`/services/${task.ServiceID}`}
        />
        <ResourceLink
          label="Node"
          name={nodeLabel}
          to={`/nodes/${task.NodeID}`}
        />
        <InfoCard
          label="Slot"
          value={task.Slot ? String(task.Slot) : "\u2014"}
        />
        <ContainerImage image={task.Spec.ContainerSpec.Image} />
        <Timestamp
          label="Timestamp"
          date={task.Status.Timestamp}
        />
        <ResourceId
          label="Container"
          id={containerId}
          truncate={12}
        />

        {exitCode != null && exitCode !== 0 && (
          <InfoCard
            label="Exit Code"
            value={String(exitCode)}
          />
        )}
      </div>

      {task.Status.Err && (
        <div className="mb-6 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-900 dark:bg-red-950/30">
          <div className="mb-1 text-xs font-medium tracking-wider text-red-600 uppercase dark:text-red-400">
            Error
          </div>
          <div className="text-sm text-red-700 dark:text-red-300">{task.Status.Err}</div>
        </div>
      )}

      {task.Status.Message && (
        <div className="mb-6 rounded-lg border bg-card p-4">
          <div className="mb-1 text-xs font-medium tracking-wider text-muted-foreground uppercase">
            Status Message
          </div>
          <div className="text-sm">{task.Status.Message}</div>
        </div>
      )}

      {hasCadvisor && task.Status.State === "running" && myMetrics && (
        <div className="mb-6 flex items-center justify-center gap-8">
          <ResourceGauge
            label="CPU"
            value={cpuGaugePercent(myMetrics.currentCpu, service)}
            subtitle={
              myMetrics.currentCpu != null ? formatPercentage(myMetrics.currentCpu) : undefined
            }
          />
          <ResourceGauge
            label="Memory"
            value={memGaugePercent(myMetrics.currentMemory, service)}
            subtitle={
              myMetrics.currentMemory != null ? formatBytes(myMetrics.currentMemory) : undefined
            }
          />
        </div>
      )}

      {hasPrometheus && task.Status.State === "running" && (
        <ErrorBoundary inline>
          <MetricsPanel
            header="Task Metrics"
            charts={[
              {
                title: "CPU Usage",
                query: `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_task_id="${escapePromQL(
                  id!,
                )}"}[5m])) * 100`,
                unit: "%",
                yMin: 0,
              },
              {
                title: "Memory Usage",
                query: `sum(container_memory_usage_bytes{container_label_com_docker_swarm_task_id="${escapePromQL(id!)}"})`,
                unit: "bytes",
                yMin: 0,
                color: "#34d399",
              },
            ]}
          />
        </ErrorBoundary>
      )}

      <div className="mb-6">
        <ErrorBoundary inline>
          <LogViewer
            taskId={id!}
            header="Logs"
          />
        </ErrorBoundary>
      </div>
    </div>
  );
}

function cpuGaugePercent(currentCpu: number | null, service: Service | null): number | null {
  if (currentCpu == null) {
    return null;
  }
  const limitNano = service?.Spec.TaskTemplate.Resources?.Limits?.NanoCPUs;
  if (!limitNano) {
    return null;
  }
  // currentCpu is % of 1 vCPU (e.g. 150 = 1.5 cores). Convert limit from
  // nanoseconds to the same unit: 1e9 nano = 1 core = 100%.
  return currentCpu / (limitNano / 1e7);
}

function memGaugePercent(currentMemory: number | null, service: Service | null): number | null {
  if (currentMemory == null) {
    return null;
  }
  const limitBytes = service?.Spec.TaskTemplate.Resources?.Limits?.MemoryBytes;
  if (!limitBytes) {
    return null;
  }
  return (currentMemory / limitBytes) * 100;
}
