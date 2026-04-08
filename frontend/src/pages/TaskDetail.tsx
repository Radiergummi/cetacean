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
import ResourceName from "../components/ResourceName";
import { Spinner } from "../components/Spinner";
import TaskStatusBadge from "../components/TaskStatusBadge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "../components/ui/alert-dialog";
import { Button } from "../components/ui/button";
import { useAsyncAction } from "../hooks/useAsyncAction";
import { useDetailResource } from "../hooks/useDetailResource";
import {
  isCadvisorReady,
  isPrometheusReady,
  useMonitoringStatus,
} from "../hooks/useMonitoringStatus";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { getSemanticChartColor } from "../lib/chartColors";
import { formatBytes, formatPercentage } from "../lib/format";
import { cpuGaugePercent, memoryGaugePercent } from "../lib/resourceGauge";
import { escapePromQL } from "../lib/utils";
import { Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

export default function TaskDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const {
    data: task,
    error,
    allowedMethods,
  } = useDetailResource<Task>(id, api.task, `/tasks/${id}`, { history: false });

  const [service, setService] = useState<Service | null>(null);
  const canRemove = allowedMethods.has("DELETE");
  const removal = useAsyncAction({ toast: true });

  // Fetch service data once when we learn the ServiceID (stable for a task's lifetime)
  const serviceId = task?.ServiceID;
  useEffect(() => {
    if (!serviceId) {
      return;
    }

    api
      .service(serviceId)
      .then(({ data: { service } }) => setService(service))
      .catch(console.warn);
  }, [serviceId]);

  function executeRemove() {
    if (!id) {
      return;
    }

    void removal.execute(async () => {
      await api.removeTask(id);
      navigate(task?.ServiceID ? `/services/${task.ServiceID}` : "/tasks");
    }, "Failed to remove task");
  }

  const monitoring = useMonitoringStatus();
  const hasCadvisor = isCadvisorReady(monitoring);
  const hasPrometheus = isPrometheusReady(monitoring);
  const taskMetrics = useTaskMetrics(
    id ? `container_label_com_docker_swarm_task_id="${escapePromQL(id)}"` : "",
    hasCadvisor && !!id && task?.Status.State === "running",
  );
  const myMetrics = id ? taskMetrics[id] : undefined;

  if (error) {
    return <FetchError message="Failed to load task" />;
  }
  if (!task) {
    return <LoadingDetail />;
  }

  const serviceName = task.ServiceName || task.ServiceID.slice(0, 12);
  const nodeLabel = task.NodeHostname || task.NodeID?.slice(0, 12) || "—";
  const taskIdShort = task.ID.slice(0, 12);
  const exitCode = task.Status.ContainerStatus?.ExitCode;
  const containerId = task.Status.ContainerStatus?.ContainerID;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          task.Slot ? (
            <span>
              <ResourceName name={serviceName} /> Replica #{task.Slot}
            </span>
          ) : (
            <>
              Task <span className="font-mono">{taskIdShort}</span>
            </>
          )
        }
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: <ResourceName name={serviceName} />, to: `/services/${task.ServiceID}` },
          {
            label: task.Slot ? (
              `Replica #${task.Slot}`
            ) : (
              <span className="font-mono">{taskIdShort}</span>
            ),
          },
        ]}
        actions={
          canRemove ? (
            <>
              <AlertDialog>
                <AlertDialogTrigger
                  render={
                    <Button
                      variant="destructive"
                      size="sm"
                    >
                      {removal.loading ? (
                        <Spinner className="size-3" />
                      ) : (
                        <Trash2 className="size-3.5" />
                      )}
                      Remove
                    </Button>
                  }
                />
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Force-remove this task?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This will kill the backing container. The service scheduler will start a
                      replacement.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction
                      onClick={() => void executeRemove()}
                      variant="destructive"
                    >
                      Remove
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </>
          ) : undefined
        }
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
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
          name={<ResourceName name={serviceName} />}
          to={`/services/${task.ServiceID}`}
        />
        <ResourceLink
          label="Node"
          name={nodeLabel}
          to={task.NodeID ? `/nodes/${task.NodeID}` : undefined}
        />
        <InfoCard
          label="Slot"
          value={task.Slot ? String(task.Slot) : "\u2014"}
        />
        <ContainerImage image={task.Spec.ContainerSpec?.Image ?? ""} />
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

        {task.Status.Err && (
          <div className="col-span-full rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-900 dark:bg-red-950/30">
            <div className="mb-1 text-xs font-medium tracking-wider text-red-600 uppercase dark:text-red-400">
              Error
            </div>
            <div className="text-sm text-red-700 dark:text-red-300">{task.Status.Err}</div>
          </div>
        )}

        {task.Status.Message && (
          <div className="col-span-full rounded-lg border bg-card p-4">
            <div className="mb-1 text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Status Message
            </div>
            <div className="text-sm">{task.Status.Message}</div>
          </div>
        )}
      </div>

      {hasCadvisor && task.Status.State === "running" && myMetrics && (
        <div className="flex items-center justify-center gap-8">
          <ResourceGauge
            label="CPU"
            value={cpuGaugePercent(
              myMetrics.currentCpu,
              service?.Spec.TaskTemplate.Resources?.Limits?.NanoCPUs,
            )}
            subtitle={
              myMetrics.currentCpu != null ? formatPercentage(myMetrics.currentCpu) : undefined
            }
          />
          <ResourceGauge
            label="Memory"
            value={memoryGaugePercent(
              myMetrics.currentMemory,
              service?.Spec.TaskTemplate.Resources?.Limits?.MemoryBytes,
            )}
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
                color: getSemanticChartColor("memory"),
              },
            ]}
          />
        </ErrorBoundary>
      )}

      <ErrorBoundary inline>
        <LogViewer
          taskId={id!}
          header="Logs"
        />
      </ErrorBoundary>
    </div>
  );
}
