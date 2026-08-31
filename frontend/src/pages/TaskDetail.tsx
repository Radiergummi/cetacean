import { api } from "../api/client";
import type { Node, Service, Task } from "../api/types";
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
import { stackNamespaceLabel } from "../lib/parseStackLabels";
import { resourceBreadcrumbs } from "../lib/resourceBreadcrumbs";
import { cpuGaugePercent, memoryGaugePercent } from "../lib/resourceGauge";
import { isTerminalTaskState } from "../lib/taskState";
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
  const [node, setNode] = useState<Node | null>(null);
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

  // Node capacity is used as the gauge denominator when no per-task limit is set.
  const nodeId = task?.NodeID;
  useEffect(() => {
    if (!nodeId) {
      return;
    }

    api
      .node(nodeId)
      .then(({ data }) => setNode(data))
      .catch(console.warn);
  }, [nodeId]);

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
    hasCadvisor && !!id && task?.Status?.State === "running",
  );
  const myMetrics = id ? taskMetrics[id] : undefined;

  if (error) {
    return <FetchError message="Failed to load task" />;
  }
  if (!task) {
    return <LoadingDetail />;
  }

  const serviceName = task.ServiceName || task.ServiceID.slice(0, 12);
  // Until the service loads there are no labels to consult, so the name keeps
  // its guessed split; once it arrives the label settles it either way.
  const serviceStack = service ? (service.Spec?.Labels?.[stackNamespaceLabel] ?? null) : undefined;
  const nodeLabel = task.NodeHostname || task.NodeID?.slice(0, 12) || "—";
  const taskIdShort = task.ID.slice(0, 12);
  // Status is technically a pointer type in Docker's API — every nested field
  // gets defensive optional chaining so a partial payload from SSE / a node
  // transition can't crash the page.
  const status = task.Status ?? {};
  const taskState = status.State;
  const containerId = status.ContainerStatus?.ContainerID;
  // Docker reports ContainerStatus.ExitCode even for running tasks (often -1
  // until the container terminates). Only surface it once the task has reached
  // a terminal state, otherwise it's misleading.
  const exitCode = isTerminalTaskState(taskState) ? status.ContainerStatus?.ExitCode : undefined;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={
          task.Slot ? (
            <span>
              <ResourceName
                name={serviceName}
                stack={serviceStack}
              />{" "}
              Replica #{task.Slot}
            </span>
          ) : (
            <>
              Task <span className="font-mono">{taskIdShort}</span>
            </>
          )
        }
        breadcrumbs={resourceBreadcrumbs({
          listLabel: "Services",
          listPath: "/services",
          name: serviceName,
          stack: serviceStack ?? undefined,
          to: `/services/${task.ServiceID}`,
          trail: [
            {
              label: task.Slot ? (
                `Replica #${task.Slot}`
              ) : (
                <span className="font-mono">{taskIdShort}</span>
              ),
            },
          ],
        })}
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
                      onClick={() => {
                        void executeRemove();
                      }}
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
          <TaskStatusBadge state={taskState ?? "unknown"} />
        </div>
        <InfoCard
          label="Desired State"
          value={task.DesiredState}
        />
        <ResourceLink
          label="Service"
          name={
            <ResourceName
              name={serviceName}
              stack={serviceStack}
            />
          }
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
        <ContainerImage image={task.Spec?.ContainerSpec?.Image ?? ""} />
        <Timestamp
          label="Timestamp"
          date={status.Timestamp}
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

        {status.Err && (
          <div className="col-span-full rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-900 dark:bg-red-950/30">
            <div className="mb-1 text-xs font-medium tracking-wider text-red-600 uppercase dark:text-red-400">
              Error
            </div>
            <div className="text-sm text-red-700 dark:text-red-300">{status.Err}</div>
          </div>
        )}

        {status.Message && (
          <div className="col-span-full rounded-lg border bg-card p-4">
            <div className="mb-1 text-xs font-medium tracking-wider text-muted-foreground uppercase">
              Status Message
            </div>
            <div className="text-sm">{status.Message}</div>
          </div>
        )}
      </div>

      {hasCadvisor && taskState === "running" && myMetrics && (
        <TaskResourceGauges
          metrics={myMetrics}
          cpuLimit={service?.Spec?.TaskTemplate?.Resources?.Limits?.NanoCPUs}
          memoryLimit={service?.Spec?.TaskTemplate?.Resources?.Limits?.MemoryBytes}
          nodeCpuCapacity={node?.Description?.Resources?.NanoCPUs}
          nodeMemoryCapacity={node?.Description?.Resources?.MemoryBytes}
        />
      )}

      {hasPrometheus && taskState === "running" && (
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

interface TaskMetricsShape {
  currentCpu: number | null;
  currentMemory: number | null;
}

/**
 * Renders CPU + memory gauges for a running task. Falls back to the host
 * node's capacity when the service has no explicit per-task limit, so the
 * gauges always have a meaningful denominator and don't render empty.
 */
function TaskResourceGauges({
  metrics,
  cpuLimit,
  memoryLimit,
  nodeCpuCapacity,
  nodeMemoryCapacity,
}: {
  metrics: TaskMetricsShape;
  cpuLimit: number | undefined;
  memoryLimit: number | undefined;
  nodeCpuCapacity: number | undefined;
  nodeMemoryCapacity: number | undefined;
}) {
  const cpuDenominator = cpuLimit || nodeCpuCapacity;
  const memDenominator = memoryLimit || nodeMemoryCapacity;

  return (
    <div className="flex items-center justify-center gap-8">
      <ResourceGauge
        label="CPU"
        value={cpuGaugePercent(metrics.currentCpu, cpuDenominator)}
        subtitle={
          metrics.currentCpu != null
            ? cpuLimit
              ? formatPercentage(metrics.currentCpu)
              : `${formatPercentage(metrics.currentCpu)} of node`
            : undefined
        }
      />
      <ResourceGauge
        label="Memory"
        value={memoryGaugePercent(metrics.currentMemory, memDenominator)}
        subtitle={
          metrics.currentMemory != null
            ? memoryLimit
              ? formatBytes(metrics.currentMemory)
              : `${formatBytes(metrics.currentMemory)} of node`
            : undefined
        }
      />
    </div>
  );
}
