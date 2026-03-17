import { api } from "../api/client";
import type { HistoryEntry, Service, SpecChange, Task } from "../api/types";
import ActivityFeed from "../components/ActivityFeed";
import CollapsibleSection from "../components/CollapsibleSection";
import { ContainerImage, KVTable, MetadataGrid, ResourceLink, Timestamp } from "../components/data";
import ErrorBoundary from "../components/ErrorBoundary";
import FetchError from "../components/FetchError";
import InfoCard from "../components/InfoCard";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { LogViewer } from "../components/log";
import { MetricsPanel, ResourceAllocationChart, type Threshold } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import SimpleTable from "../components/SimpleTable";
import TasksTable from "../components/TasksTable";
import { timeAgo } from "../components/TimeAgo";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { formatBytes } from "../lib/formatBytes";
import { formatNs } from "../lib/formatNs";
import { escapePromQL } from "../lib/utils";
import { ArrowRight, Globe, ImageIcon, Pencil, Plus, RefreshCw, RotateCcw, Shuffle, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";

export default function ServiceDetail() {
  const { id } = useParams<{ id: string }>();
  const [service, setService] = useState<Service | null>(null);
  const [changes, setChanges] = useState<SpecChange[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [envVars, setEnvVars] = useState<Record<string, string> | null>(null);
  const [serviceResources, setServiceResources] = useState<Record<string, unknown> | null>(null);
  const monitoring = useMonitoringStatus();
  const hasPrometheus = monitoring?.prometheusConfigured && monitoring?.prometheusReachable;
  const hasCadvisor = !!monitoring?.cadvisor?.targets;
  const [error, setError] = useState(false);
  const [networkNames, setNetworkNames] = useState<Record<string, string>>({});
  const [cpuActual, setCpuActual] = useState<number | undefined>();
  const [memActual, setMemActual] = useState<number | undefined>();

  const fetchData = useCallback(() => {
    if (!id) {
      return;
    }

    api
      .service(id)
      .then((r) => {
        setService(r.service);
        setChanges(r.changes ?? []);
      })
      .catch(() => setError(true));
    api
      .serviceTasks(id)
      .then(setTasks)
      .catch(() => {});
    api
      .history({ resourceId: id, limit: 10 })
      .then(setHistory)
      .catch(() => {});
    api
      .serviceEnv(id)
      .then(setEnvVars)
      .catch(() => {});
    api
      .serviceResources(id)
      .then(setServiceResources)
      .catch(() => {});
  }, [id]);

  useEffect(() => {
    api
      .networks({ limit: 0 })
      .then((res) => {
        const map: Record<string, string> = {};
        for (const n of res.items) {
          map[n.Id] = n.Name;
        }
        setNetworkNames(map);
      })
      .catch(() => {});
  }, []);

  useEffect(fetchData, [fetchData]);

  useResourceStream(`/services/${id}`, fetchData);

  const serviceName = service?.Spec.Name || "";
  const taskMetrics = useTaskMetrics(
    serviceName
      ? `container_label_com_docker_swarm_service_name="${escapePromQL(serviceName)}"`
      : "",
    hasCadvisor && !!serviceName,
  );

  useEffect(() => {
    if (!serviceName || !hasCadvisor) return;
    let cancelled = false;
    const escaped = escapePromQL(serviceName);

    Promise.all([
      api.metricsQuery(
        `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${escaped}"}[5m])) * 100`,
      ),
      api.metricsQuery(
        `sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${escaped}"})`,
      ),
    ])
      .then(([cpuResp, memResp]) => {
        if (cancelled) return;
        const cpuVal = cpuResp.data?.result?.[0]?.value?.[1];
        const memVal = memResp.data?.result?.[0]?.value?.[1];
        if (cpuVal != null) setCpuActual(Number(cpuVal));
        if (memVal != null) setMemActual(Number(memVal));
      })
      .catch(() => {});

    return () => {
      cancelled = true;
    };
  }, [serviceName, hasCadvisor]);

  const name = service?.Spec.Name || service?.ID || "";

  const metricsCharts = useMemo(
    () =>
      service
        ? [
            {
              title: "CPU Usage",
              query: `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${escapePromQL(name)}"}[5m])) * 100`,
              unit: "%",
              thresholds: cpuThresholds(service),
              yMin: 0,
            },
            {
              title: "Memory Usage",
              query: `sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${escapePromQL(name)}"})`,
              unit: "bytes",
              thresholds: memoryThresholds(service),
              yMin: 0,
              color: "#34d399",
            },
          ]
        : [],
    [name, service],
  );

  if (error) {
    return <FetchError message="Failed to load service" />;
  }

  if (!service) {
    return <LoadingDetail />;
  }

  const containerSpec = service.Spec.TaskTemplate.ContainerSpec;
  const taskTemplate = service.Spec.TaskTemplate;
  const labels = service.Spec.Labels;
  const nonStackLabels = Object.entries(labels).filter(
    ([key]) => !key.startsWith("com.docker.stack."),
  );

  const hasContainerConfig =
    containerSpec.Command ||
    containerSpec.Args ||
    containerSpec.User ||
    containerSpec.Dir ||
    containerSpec.Hostname ||
    containerSpec.StopSignal ||
    containerSpec.StopGracePeriod != null ||
    containerSpec.Init != null ||
    containerSpec.ReadOnly;

  const runningTasks = tasks?.filter((t) => t.Status?.State === "running").length ?? 0;
  const resources = service?.Spec?.TaskTemplate?.Resources;
  const cpuReserved = resources?.Reservations?.NanoCPUs
    ? (resources.Reservations.NanoCPUs / 1e9) * 100 * runningTasks
    : undefined;
  const cpuLimit = resources?.Limits?.NanoCPUs
    ? (resources.Limits.NanoCPUs / 1e9) * 100 * runningTasks
    : undefined;
  const memReserved = resources?.Reservations?.MemoryBytes
    ? resources.Reservations.MemoryBytes * runningTasks
    : undefined;
  const memLimit = resources?.Limits?.MemoryBytes
    ? resources.Limits.MemoryBytes * runningTasks
    : undefined;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<ResourceName name={name} />}
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: <ResourceName name={name} /> },
        ]}
      />

      <ServiceActions
        service={service}
        serviceId={id!}
      />

      {/* Overview cards */}
      <MetadataGrid>
        <ContainerImage image={containerSpec.Image} />
        <ReplicaCard
          service={service}
          tasks={tasks}
        />
        <ServiceStatusCard service={service} />
        <ResourceLink
          label="Stack"
          name={labels["com.docker.stack.namespace"]}
          to={`/stacks/${labels["com.docker.stack.namespace"]}`}
        />
        <Timestamp
          label="Created"
          date={service.CreatedAt}
        />
        <Timestamp
          label="Updated"
          date={service.UpdatedAt}
        />
      </MetadataGrid>

      {/* Last Deployment */}
      {changes.length > 0 && (
        <CollapsibleSection
          title="Last Deployment"
          defaultOpen={service.UpdateStatus?.State === "updating"}
        >
          <DeploymentChanges
            changes={changes}
            updateStatus={service.UpdateStatus}
          />
        </CollapsibleSection>
      )}

      {/* Tasks */}
      <TasksTable
        tasks={tasks}
        variant="service"
        metrics={hasCadvisor ? taskMetrics : undefined}
      />

      {hasPrometheus && (
        <ErrorBoundary inline>
          <MetricsPanel
            header="Metrics"
            charts={metricsCharts}
          />
        </ErrorBoundary>
      )}

      {(cpuReserved != null || cpuLimit != null || memReserved != null || memLimit != null) && (
        <CollapsibleSection title="Resource Allocation">
          <ResourceAllocationChart
            cpuReserved={cpuReserved}
            cpuLimit={cpuLimit}
            cpuActual={cpuActual}
            memReserved={memReserved}
            memLimit={memLimit}
            memActual={memActual}
          />
        </CollapsibleSection>
      )}

      {/* Resources editor */}
      {serviceResources !== null && (
        <ResourcesEditor
          serviceId={id!}
          resources={serviceResources}
          onSaved={setServiceResources}
        />
      )}

      {/* Container configuration */}
      {hasContainerConfig && (
        <CollapsibleSection
          title="Container Configuration"
          defaultOpen={false}
        >
          <KVTable
            rows={[
              containerSpec.Command && ["Command", containerSpec.Command.join(" ")],
              containerSpec.Args && ["Args", containerSpec.Args.join(" ")],
              containerSpec.User && ["User", containerSpec.User],
              containerSpec.Dir && ["Working Dir", containerSpec.Dir],
              containerSpec.Hostname && ["Hostname", containerSpec.Hostname],
              containerSpec.StopSignal && ["Stop Signal", containerSpec.StopSignal],
              containerSpec.StopGracePeriod != null && [
                "Stop Grace Period",
                formatNs(containerSpec.StopGracePeriod),
              ],
              containerSpec.Init != null && ["Init", containerSpec.Init ? "yes" : "no"],
              containerSpec.ReadOnly && ["Read Only Root FS", "yes"],
            ]}
          />
        </CollapsibleSection>
      )}

      {/* Environment variables */}
      {envVars !== null && (
        <EnvEditor
          serviceId={id!}
          envVars={envVars}
          onSaved={setEnvVars}
        />
      )}

      {/* Healthcheck */}
      {containerSpec.Healthcheck && (
        <CollapsibleSection
          title="Healthcheck"
          defaultOpen={false}
        >
          <KVTable
            rows={[
              containerSpec.Healthcheck.Test && ["Test", containerSpec.Healthcheck.Test.join(" ")],
              containerSpec.Healthcheck.Interval != null && [
                "Interval",
                formatNs(containerSpec.Healthcheck.Interval),
              ],
              containerSpec.Healthcheck.Timeout != null && [
                "Timeout",
                formatNs(containerSpec.Healthcheck.Timeout),
              ],
              containerSpec.Healthcheck.Retries != null && [
                "Retries",
                String(containerSpec.Healthcheck.Retries),
              ],
              containerSpec.Healthcheck.StartPeriod != null && [
                "Start Period",
                formatNs(containerSpec.Healthcheck.StartPeriod),
              ],
            ]}
          />
        </CollapsibleSection>
      )}

      {/* Labels */}
      {nonStackLabels.length > 0 && (
        <CollapsibleSection
          title="Labels"
          defaultOpen={false}
        >
          <div className="flex flex-wrap gap-2">
            {nonStackLabels.map(([key, value]) => (
              <span
                key={key}
                className="inline-flex items-center rounded-md border px-2 py-0.5 font-mono text-xs"
              >
                <span className="text-muted-foreground">{key}=</span>
                {value}
              </span>
            ))}
          </div>
        </CollapsibleSection>
      )}

      {/* Ports */}
      {service.Endpoint?.Ports && service.Endpoint.Ports.length > 0 && (
        <CollapsibleSection
          title="Ports"
          defaultOpen={false}
        >
          <div className="flex flex-wrap gap-2">
            {service.Endpoint.Ports.map(
              ({ Protocol, PublishMode, PublishedPort, TargetPort }, index) => (
                <span
                  key={index}
                  className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 font-mono text-sm"
                >
                  <span className="font-semibold">{PublishedPort}</span>
                  <ArrowRight className="size-3 text-muted-foreground" />
                  <span>
                    {TargetPort}/{Protocol}
                  </span>
                  <span className="text-xs text-muted-foreground">({PublishMode})</span>
                </span>
              ),
            )}
          </div>
        </CollapsibleSection>
      )}

      {/* Mounts */}
      {containerSpec.Mounts && containerSpec.Mounts.length > 0 && (
        <CollapsibleSection
          title="Mounts"
          defaultOpen={false}
        >
          <SimpleTable
            columns={["Type", "Source", "Target", "Read Only"]}
            items={containerSpec.Mounts}
            keyFn={(_, i) => i}
            renderRow={({ ReadOnly, Source, Target, Type }) => (
              <>
                <td className="p-3 text-sm">
                  <MountTypeBadge type={Type} />
                </td>
                <td className="p-3 font-mono text-xs">
                  {Type === "volume" && Source ? (
                    <Link
                      to={`/volumes/${Source}`}
                      className="text-link hover:underline"
                    >
                      <ResourceName name={Source} />
                    </Link>
                  ) : (
                    Source || "\u2014"
                  )}
                </td>
                <td className="p-3 font-mono text-xs">{Target}</td>
                <td className="p-3 text-sm">{ReadOnly ? "yes" : "no"}</td>
              </>
            )}
          />
        </CollapsibleSection>
      )}

      {/* Networks */}
      {taskTemplate.Networks && taskTemplate.Networks.length > 0 && (
        <CollapsibleSection
          title="Networks"
          defaultOpen={false}
        >
          <SimpleTable
            columns={["Network", "Virtual IP", "Aliases"]}
            items={taskTemplate.Networks}
            keyFn={({ Target }) => Target}
            renderRow={({ Aliases, Target }) => {
              const vip = service.Endpoint?.VirtualIPs?.find((v) => v.NetworkID === Target);
              return (
                <>
                  <td className="p-3 text-sm">
                    <Link
                      to={`/networks/${Target}`}
                      className="text-link hover:underline"
                    >
                      <ResourceName name={networkNames[Target] || Target} />
                    </Link>
                  </td>
                  <td className="p-3 font-mono text-xs">{vip?.Addr || "\u2014"}</td>
                  <td className="p-3 font-mono text-xs">{Aliases?.join(", ") || "\u2014"}</td>
                </>
              );
            }}
          />
        </CollapsibleSection>
      )}

      {/* Configs */}
      {containerSpec.Configs && containerSpec.Configs.length > 0 && (
        <CollapsibleSection
          title="Configs"
          defaultOpen={false}
        >
          <SimpleTable
            columns={["Name", "Target"]}
            items={containerSpec.Configs}
            keyFn={({ ConfigID }) => ConfigID}
            renderRow={({ ConfigID, ConfigName, File }) => (
              <>
                <td className="p-3 text-sm">
                  <Link
                    to={`/configs/${ConfigID}`}
                    className="text-link hover:underline"
                  >
                    <ResourceName name={ConfigName} />
                  </Link>
                </td>
                <td className="p-3 font-mono text-xs">{File?.Name ?? "\u2014"}</td>
              </>
            )}
          />
        </CollapsibleSection>
      )}

      {/* Secrets */}
      {containerSpec.Secrets && containerSpec.Secrets.length > 0 && (
        <CollapsibleSection
          title="Secrets"
          defaultOpen={false}
        >
          <SimpleTable
            columns={["Name", "Target"]}
            items={containerSpec.Secrets}
            keyFn={({ SecretID }) => SecretID}
            renderRow={({ File, SecretID, SecretName }) => (
              <>
                <td className="p-3 text-sm">
                  <Link
                    to={`/secrets/${SecretID}`}
                    className="text-link hover:underline"
                  >
                    <ResourceName name={SecretName} />
                  </Link>
                </td>
                <td className="p-3 font-mono text-xs">{File?.Name || "\u2014"}</td>
              </>
            )}
          />
        </CollapsibleSection>
      )}

      {/* Deploy: Resources, Placement, Restart, Update, Rollback */}
      <CollapsibleSection
        title="Deploy Configuration"
        defaultOpen={false}
      >
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {taskTemplate.Resources && (
            <div className="flex flex-col gap-3 rounded-lg border p-4">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Resources
              </h3>

              <ResourcesPanel resources={taskTemplate.Resources} />
            </div>
          )}

          {taskTemplate.Placement && (
            <div className="flex flex-col gap-3 rounded-lg border p-4">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Placement
              </h3>

              <PlacementPanel placement={taskTemplate.Placement} />
            </div>
          )}

          {taskTemplate.RestartPolicy && (
            <div className="flex flex-col gap-2">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Restart Policy
              </h3>
              <KVTable
                rows={[
                  taskTemplate.RestartPolicy.Condition && [
                    "Condition",
                    taskTemplate.RestartPolicy.Condition,
                  ],
                  taskTemplate.RestartPolicy.Delay != null && [
                    "Delay",
                    formatNs(taskTemplate.RestartPolicy.Delay),
                  ],
                  taskTemplate.RestartPolicy.MaxAttempts != null && [
                    "Max Attempts",
                    String(taskTemplate.RestartPolicy.MaxAttempts),
                  ],
                  taskTemplate.RestartPolicy.Window != null && [
                    "Window",
                    formatNs(taskTemplate.RestartPolicy.Window),
                  ],
                ]}
              />
            </div>
          )}

          {taskTemplate.LogDriver && (
            <div className="flex flex-col gap-2">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Log Driver
              </h3>
              <KVTable
                rows={[
                  ["Driver", taskTemplate.LogDriver.Name],
                  ...(taskTemplate.LogDriver.Options
                    ? Object.entries(taskTemplate.LogDriver.Options).map(
                        ([key, value]) => [key, value] as [string, string],
                      )
                    : []),
                ]}
              />
            </div>
          )}

          {service.Spec.UpdateConfig && (
            <div className="flex flex-col gap-2">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Update Config
              </h3>
              <KVTable rows={updateConfigRows(service.Spec.UpdateConfig)} />
            </div>
          )}

          {service.Spec.RollbackConfig && (
            <div className="flex flex-col gap-2">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Rollback Config
              </h3>
              <KVTable rows={updateConfigRows(service.Spec.RollbackConfig)} />
            </div>
          )}

          {service.Spec.EndpointSpec?.Mode && (
            <div className="flex flex-col gap-3 rounded-lg border p-4">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Endpoint Mode
              </h3>

              <div className="flex items-center gap-2 text-sm">
                {service.Spec.EndpointSpec.Mode === "vip" ? (
                  <>
                    <Globe className="h-4 w-4 text-muted-foreground" /> VIP (Virtual IP)
                  </>
                ) : (
                  <>
                    <Shuffle className="h-4 w-4 text-muted-foreground" /> DNS-RR (DNS Round Robin)
                  </>
                )}
              </div>
            </div>
          )}
        </div>
      </CollapsibleSection>

      {history.length > 0 && (
        <CollapsibleSection title="Recent Activity">
          <ActivityFeed entries={history} />
        </CollapsibleSection>
      )}

      <ErrorBoundary inline>
        <LogViewer
          serviceId={id!}
          header="Logs"
        />
      </ErrorBoundary>
    </div>
  );
}

const statusStyles: Record<string, string> = {
  stable: "text-green-600 dark:text-green-400",
  updating: "text-blue-600 dark:text-blue-400",
  rollback_started: "text-amber-600 dark:text-amber-400",
  paused: "text-amber-600 dark:text-amber-400",
  rollback_paused: "text-amber-600 dark:text-amber-400",
  rollback_completed: "text-amber-600 dark:text-amber-400",
};

const statusLabels: Record<string, string> = {
  stable: "Stable",
  updating: "Updating",
  completed: "Stable",
  paused: "Paused",
  rollback_started: "Rolling back",
  rollback_paused: "Rollback paused",
  rollback_completed: "Rolled back",
};

function serviceStatusLabel(service: Service): { label: string; color: string } {
  const state = service.UpdateStatus?.State;
  if (!state || state === "completed") {
    return { label: "Stable", color: statusStyles.stable };
  }
  return {
    label: statusLabels[state] || state,
    color: statusStyles[state] || statusStyles.stable,
  };
}

function ServiceStatusCard({ service }: { service: Service }) {
  const { label, color } = serviceStatusLabel(service);
  const ts = service.UpdateStatus?.CompletedAt || service.UpdateStatus?.StartedAt;
  const msg = service.UpdateStatus?.Message;

  return (
    <InfoCard
      label="Status"
      value={
        <div className="flex flex-col">
          <span className={`text-base font-medium ${color}`}>{label}</span>
          {ts && <span className="text-xs text-muted-foreground">{timeAgo(ts)}</span>}
          {msg && label !== "Stable" && (
            <span
              className="truncate text-xs text-muted-foreground"
              title={msg}
            >
              {msg}
            </span>
          )}
        </div>
      }
    />
  );
}

type UpdateConfigShape = NonNullable<Service["Spec"]["UpdateConfig"]>;

function updateConfigRows(cfg: UpdateConfigShape) {
  return [
    ["Parallelism", String(cfg.Parallelism)] as [string, string],
    cfg.Delay != null && (["Delay", formatNs(cfg.Delay)] as [string, string]),
    cfg.FailureAction && (["Failure Action", cfg.FailureAction] as [string, string]),
    cfg.Monitor != null && (["Monitor", formatNs(cfg.Monitor)] as [string, string]),
    cfg.MaxFailureRatio != null &&
      (["Max Failure Ratio", String(cfg.MaxFailureRatio)] as [string, string]),
    cfg.Order && (["Order", cfg.Order] as [string, string]),
  ];
}

function Spinner() {
  return (
    <svg
      className="h-3 w-3 animate-spin"
      xmlns="http://www.w3.org/2000/svg"
      fill="none"
      viewBox="0 0 24 24"
    >
      <circle
        className="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        strokeWidth="4"
      />
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
      />
    </svg>
  );
}

function ServiceActions({ service, serviceId }: { service: Service; serviceId: string }) {
  const currentImage = service.Spec.TaskTemplate.ContainerSpec.Image;
  // Strip digest suffix for display/editing
  const imageWithoutDigest = currentImage.replace(/@sha256:[a-f0-9]+$/, "");

  const [imageOpen, setImageOpen] = useState(false);
  const [imageValue, setImageValue] = useState("");
  const [imageLoading, setImageLoading] = useState(false);
  const [imageError, setImageError] = useState<string | null>(null);

  const [rollbackLoading, setRollbackLoading] = useState(false);
  const [rollbackError, setRollbackError] = useState<string | null>(null);

  const [restartLoading, setRestartLoading] = useState(false);
  const [restartError, setRestartError] = useState<string | null>(null);

  const canRollback = !!service.PreviousSpec;

  function openImage() {
    setImageValue(imageWithoutDigest);
    setImageError(null);
    setImageOpen(true);
  }

  function cancelImage() {
    setImageOpen(false);
    setImageError(null);
  }

  async function submitImage() {
    const trimmed = imageValue.trim();
    if (!trimmed) {
      setImageError("Enter an image name");
      return;
    }
    setImageLoading(true);
    setImageError(null);
    try {
      await api.updateServiceImage(serviceId, trimmed);
      setImageOpen(false);
    } catch (err) {
      setImageError(err instanceof Error ? err.message : "Failed to update image");
    } finally {
      setImageLoading(false);
    }
  }

  async function handleRollback() {
    if (!window.confirm("Are you sure you want to rollback this service?")) return;
    setRollbackLoading(true);
    setRollbackError(null);
    try {
      await api.rollbackService(serviceId);
    } catch (err) {
      setRollbackError(err instanceof Error ? err.message : "Failed to rollback");
    } finally {
      setRollbackLoading(false);
    }
  }

  async function handleRestart() {
    if (!window.confirm("Are you sure you want to restart this service? This triggers a rolling restart.")) return;
    setRestartLoading(true);
    setRestartError(null);
    try {
      await api.restartService(serviceId);
    } catch (err) {
      setRestartError(err instanceof Error ? err.message : "Failed to restart");
    } finally {
      setRestartLoading(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* Update Image */}
      <div className="relative">
        <button
          type="button"
          onClick={openImage}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:opacity-50"
        >
          <ImageIcon className="h-3.5 w-3.5" />
          Update Image
        </button>
        {imageOpen && (
          <div className="absolute left-0 top-full z-50 mt-1 w-80 rounded-lg border bg-card p-3 shadow-lg">
            <p className="mb-1 text-xs font-medium text-muted-foreground">New image</p>
            <p className="mb-2 truncate font-mono text-xs text-muted-foreground" title={currentImage}>
              Current: {imageWithoutDigest}
            </p>
            <input
              type="text"
              value={imageValue}
              onChange={(e) => setImageValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void submitImage();
                if (e.key === "Escape") cancelImage();
              }}
              placeholder="image:tag"
              className="mb-2 w-full rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
              autoFocus
            />
            {imageError && (
              <p className="mb-2 text-xs text-red-600 dark:text-red-400">{imageError}</p>
            )}
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => void submitImage()}
                disabled={imageLoading}
                className="flex flex-1 items-center justify-center gap-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
              >
                {imageLoading && <Spinner />}
                Update
              </button>
              <button
                type="button"
                onClick={cancelImage}
                disabled={imageLoading}
                className="flex-1 rounded border px-2 py-1 text-xs font-medium disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Rollback */}
      <div className="flex flex-col items-start gap-1">
        <button
          type="button"
          onClick={() => void handleRollback()}
          disabled={!canRollback || rollbackLoading}
          title={canRollback ? "Rollback to previous spec" : "No previous spec available"}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          {rollbackLoading ? <Spinner /> : <RotateCcw className="h-3.5 w-3.5" />}
          Rollback
        </button>
        {rollbackError && (
          <p className="text-xs text-red-600 dark:text-red-400">{rollbackError}</p>
        )}
      </div>

      {/* Restart */}
      <div className="flex flex-col items-start gap-1">
        <button
          type="button"
          onClick={() => void handleRestart()}
          disabled={restartLoading}
          className="inline-flex items-center gap-1.5 rounded border px-3 py-1.5 text-sm font-medium hover:bg-accent disabled:opacity-50"
        >
          {restartLoading ? <Spinner /> : <RefreshCw className="h-3.5 w-3.5" />}
          Restart
        </button>
        {restartError && (
          <p className="text-xs text-red-600 dark:text-red-400">{restartError}</p>
        )}
      </div>
    </div>
  );
}

function ReplicaDoughnut({ running, desired }: { running: number; desired: number }) {
  const size = 50;
  const stroke = 5;
  const radius = (size - stroke) / 2;
  const circumference = 2 * Math.PI * radius;
  const ratio = desired > 0 ? Math.min(running / desired, 1) : 0;
  const offset = circumference * (1 - ratio);
  const healthy = running >= desired;

  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
    >
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        className="text-muted/30"
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        strokeDasharray={circumference}
        strokeDashoffset={offset}
        strokeLinecap="round"
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
        className={healthy ? "text-green-500" : "text-red-500"}
      />
    </svg>
  );
}

function ReplicaCard({ service, tasks }: { service: Service; tasks: Task[] }) {
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleValue, setScaleValue] = useState("");
  const [scaleLoading, setScaleLoading] = useState(false);
  const [scaleError, setScaleError] = useState<string | null>(null);

  const replicated = service.Spec.Mode.Replicated;
  if (!replicated) {
    return <InfoCard label="Mode" value="global" />;
  }

  const desired = replicated.Replicas ?? 0;
  const running = tasks.filter((t) => t.Status.State === "running").length;
  const healthy = running >= desired;

  function openScale() {
    setScaleValue(String(desired));
    setScaleError(null);
    setScaleOpen(true);
  }

  function cancelScale() {
    setScaleOpen(false);
    setScaleError(null);
  }

  async function submitScale() {
    const n = parseInt(scaleValue, 10);
    if (isNaN(n) || n < 0) {
      setScaleError("Enter a valid replica count");
      return;
    }
    setScaleLoading(true);
    setScaleError(null);
    try {
      await api.scaleService(service.ID, n);
      setScaleOpen(false);
    } catch (err) {
      setScaleError(err instanceof Error ? err.message : "Failed to scale");
    } finally {
      setScaleLoading(false);
    }
  }

  const value = (
    <>
      <span className="tabular-nums">
        <span className="text-2xl font-bold">{running}</span>
        <span className="text-lg font-normal text-muted-foreground">/{desired}</span>
      </span>

      {!healthy && (
        <div className="mt-1 text-xs text-red-600 dark:text-red-400">
          {desired - running} replica{desired - running !== 1 ? "s" : ""} not running
        </div>
      )}
    </>
  );

  const scaleControl = (
    <div className="relative flex items-center gap-2">
      {desired > 0 && <ReplicaDoughnut running={running} desired={desired} />}
      <button
        type="button"
        onClick={openScale}
        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
        title="Scale service"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
          <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
        </svg>
      </button>

      {scaleOpen && (
        <div className="absolute right-0 top-full z-50 mt-1 w-52 rounded-lg border bg-card p-3 shadow-lg">
          <p className="mb-2 text-xs font-medium text-muted-foreground">Scale replicas</p>
          <input
            type="number"
            min={0}
            value={scaleValue}
            onChange={(e) => setScaleValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void submitScale();
              if (e.key === "Escape") cancelScale();
            }}
            className="mb-2 w-full rounded border bg-background px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
            autoFocus
          />
          {scaleError && (
            <p className="mb-2 text-xs text-red-600 dark:text-red-400">{scaleError}</p>
          )}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void submitScale()}
              disabled={scaleLoading}
              className="flex flex-1 items-center justify-center gap-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {scaleLoading && (
                <svg
                  className="h-3 w-3 animate-spin"
                  xmlns="http://www.w3.org/2000/svg"
                  fill="none"
                  viewBox="0 0 24 24"
                >
                  <circle
                    className="opacity-25"
                    cx="12"
                    cy="12"
                    r="10"
                    stroke="currentColor"
                    strokeWidth="4"
                  />
                  <path
                    className="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"
                  />
                </svg>
              )}
              Scale
            </button>
            <button
              type="button"
              onClick={cancelScale}
              disabled={scaleLoading}
              className="flex-1 rounded border px-2 py-1 text-xs font-medium disabled:opacity-50"
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </div>
  );

  return (
    <InfoCard
      label="Replicas"
      value={value}
      right={scaleControl}
    />
  );
}

const mountTypeColors: Record<string, string> = {
  volume: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300",
  bind: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300",
  tmpfs: "bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300",
};

function MountTypeBadge({ type }: { type: string }) {
  const color = mountTypeColors[type] || "bg-muted text-muted-foreground";
  return (
    <span
      className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${color}`}
    >
      {type}
    </span>
  );
}

function humanizeConstraint(raw: string): { label: string; exclude: boolean } | null {
  const match = raw.match(/^(.+?)\s*(==|!=)\s*(.+)$/);
  if (!match) {
    return null;
  }
  const [, field, op, value] = match;
  const exclude = op === "!=";

  if (field === "node.role") {
    if (value === "manager" && !exclude) {
      return { label: "Manager nodes only", exclude };
    }
    if (value === "worker" && !exclude) {
      return { label: "Worker nodes only", exclude };
    }
    if (value === "manager" && exclude) {
      return { label: "Exclude manager nodes", exclude };
    }
    if (value === "worker" && exclude) {
      return { label: "Exclude worker nodes", exclude };
    }
  }
  if (field === "node.hostname") {
    return { label: exclude ? `Exclude node ${value}` : `Node: ${value}`, exclude };
  }
  if (field === "node.id") {
    return { label: exclude ? `Exclude node ID ${value}` : `Node ID: ${value}`, exclude };
  }
  if (field === "node.platform.os") {
    return { label: exclude ? `Exclude OS ${value}` : `OS: ${value}`, exclude };
  }
  if (field === "node.platform.arch") {
    return { label: exclude ? `Exclude arch ${value}` : `Arch: ${value}`, exclude };
  }
  if (field.startsWith("node.labels.")) {
    const key = field.slice("node.labels.".length);
    return { label: exclude ? `${key} \u2260 ${value}` : `${key} = ${value}`, exclude };
  }
  if (field.startsWith("engine.labels.")) {
    const key = field.slice("engine.labels.".length);
    return {
      label: exclude ? `engine ${key} \u2260 ${value}` : `engine ${key} = ${value}`,
      exclude,
    };
  }
  return null;
}

type PlacementShape = NonNullable<Service["Spec"]["TaskTemplate"]["Placement"]>;

function PlacementPanel({ placement }: { placement: PlacementShape }) {
  const constraints = placement.Constraints ?? [];
  const preferences = placement.Preferences ?? [];
  const hasContent = constraints.length > 0 || placement.MaxReplicas || preferences.length > 0;

  if (!hasContent) {
    return <p className="text-sm text-muted-foreground">No placement constraints.</p>;
  }

  return (
    <div className="space-y-3">
      {constraints.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {constraints.map((c) => {
            const humanized = humanizeConstraint(c);
            return (
              <span
                key={c}
                data-exclude={humanized?.exclude || undefined}
                className="inline-flex items-center rounded-lg border px-3 py-2 text-sm data-exclude:border-red-200 data-exclude:bg-red-50 data-exclude:text-red-800 dark:data-exclude:border-red-800 dark:data-exclude:bg-red-950/30 dark:data-exclude:text-red-300"
                title={c}
              >
                {humanized?.label ?? c}
              </span>
            );
          })}
        </div>
      )}

      {placement.MaxReplicas != null && placement.MaxReplicas > 0 && (
        <div className="inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm">
          <span className="text-muted-foreground">Max replicas per node:</span>
          <span className="font-semibold tabular-nums">{placement.MaxReplicas}</span>
        </div>
      )}

      {preferences.length > 0 && (
        <div className="space-y-1">
          <div className="text-xs font-medium text-muted-foreground">Spread preferences</div>
          <div className="flex flex-wrap gap-2">
            {preferences.map((p, i) => (
              <span
                key={i}
                className="inline-flex items-center rounded-md border px-2.5 py-1 font-mono text-xs"
              >
                {p.Spread?.SpreadDescriptor}
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

type ResourceShape = NonNullable<Service["Spec"]["TaskTemplate"]["Resources"]>;

function ResourceLimitsBar({
  label,
  reserved,
  limit,
  format,
}: {
  label: string;
  reserved?: number;
  limit?: number;
  format: (v: number) => string;
}) {
  if (!reserved && !limit) {
    return null;
  }
  const max = limit || reserved || 0;
  const reservedPct = reserved && max ? Math.round((reserved / max) * 100) : 0;

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-sm">
        <span className="font-medium">{label}</span>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          {reserved != null && (
            <span>
              Reserved: <span className="font-mono text-foreground">{format(reserved)}</span>
            </span>
          )}
          {limit != null && (
            <span>
              Limit: <span className="font-mono text-foreground">{format(limit)}</span>
            </span>
          )}
        </div>
      </div>
      {limit && reserved ? (
        <div className="h-2 overflow-hidden rounded-full bg-muted">
          <div
            className="h-full rounded-full bg-blue-500"
            style={{ width: `${reservedPct}%` }}
          />
        </div>
      ) : null}
    </div>
  );
}

function ResourcesPanel({ resources }: { resources: ResourceShape }) {
  const hasCpu = resources.Limits?.NanoCPUs || resources.Reservations?.NanoCPUs;
  const hasMem = resources.Limits?.MemoryBytes || resources.Reservations?.MemoryBytes;
  const hasPids = resources.Limits?.Pids;

  if (!hasCpu && !hasMem && !hasPids) {
    return null;
  }

  return (
    <div className="space-y-4">
      <ResourceLimitsBar
        label="CPU"
        reserved={resources.Reservations?.NanoCPUs}
        limit={resources.Limits?.NanoCPUs}
        format={formatCpu}
      />
      <ResourceLimitsBar
        label="Memory"
        reserved={resources.Reservations?.MemoryBytes}
        limit={resources.Limits?.MemoryBytes}
        format={formatBytes}
      />
      {hasPids && (
        <div className="flex items-center justify-between text-sm">
          <span className="font-medium">PID Limit</span>
          <span className="font-mono">{resources.Limits!.Pids}</span>
        </div>
      )}
    </div>
  );
}

function DeploymentChanges({
  changes,
  updateStatus,
}: {
  changes: SpecChange[];
  updateStatus?: Service["UpdateStatus"];
}) {
  const ts = updateStatus?.CompletedAt || updateStatus?.StartedAt;
  const deploymentLabels: Record<string, string> = {
    updating: "In progress",
    rollback_started: "Rolling back",
    rollback_paused: "Rollback paused",
    rollback_completed: "Rolled back",
  };
  const stateLabel = deploymentLabels[updateStatus?.State ?? ""] ?? "Completed";

  return (
    <div className="space-y-3">
      {ts && (
        <p className="text-sm text-muted-foreground">
          {stateLabel} {timeAgo(ts)}
        </p>
      )}
      <div className="divide-y rounded-lg border">
        {changes.map(({field, new: change, old}, index) => (
          <div
            key={index}
            className="flex items-center gap-2 px-3 py-2 text-sm"
          >
            <span className="min-w-40 shrink-0 font-medium">{field}</span>
            {old && change ? (
              <>
                <span className="font-mono text-xs text-red-600 line-through dark:text-red-400">
                  {old}
                </span>
                <ArrowRight className="size-3 shrink-0 text-muted-foreground" />
                <span className="font-mono text-xs text-green-600 dark:text-green-400">
                  {change}
                </span>
              </>
            ) : old ? (
              <span className="font-mono text-xs text-red-600 dark:text-red-400">{old}</span>
            ) : (
              <span className="font-mono text-xs text-green-600 dark:text-green-400">{change}</span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function EnvEditor({
  serviceId,
  envVars,
  onSaved,
}: {
  serviceId: string;
  envVars: Record<string, string>;
  onSaved: (updated: Record<string, string>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");
  const [newVal, setNewVal] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  function openEdit() {
    setDraft({ ...envVars });
    setNewKey("");
    setNewVal("");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  function addRow() {
    const k = newKey.trim();
    if (!k) return;
    setDraft((prev) => ({ ...prev, [k]: newVal }));
    setNewKey("");
    setNewVal("");
  }

  function removeRow(key: string) {
    if (!window.confirm(`Remove env var "${key}"?`)) return;
    setDraft((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  }

  async function save() {
    // Build JSON Patch ops
    const ops: Array<{ op: string; path: string; value?: string }> = [];
    const original = envVars;

    // Removed keys
    for (const k of Object.keys(original)) {
      if (!(k in draft)) {
        ops.push({ op: "remove", path: `/${k}` });
      }
    }
    // Added / replaced keys
    for (const [k, v] of Object.entries(draft)) {
      if (!(k in original)) {
        ops.push({ op: "add", path: `/${k}`, value: v });
      } else if (original[k] !== v) {
        ops.push({ op: "replace", path: `/${k}`, value: v });
      }
    }
    if (ops.length === 0) {
      setEditing(false);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.patchServiceEnv(serviceId, ops);
      onSaved(updated);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const entries = Object.entries(envVars).sort(([a], [b]) => a.localeCompare(b));
  const draftEntries = Object.entries(draft).sort(([a], [b]) => a.localeCompare(b));

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="h-3 w-3" />
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection
      title="Environment Variables"
      defaultOpen={false}
      controls={controls}
    >
      {!editing ? (
        entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No environment variables.</p>
        ) : (
          <SimpleTable
            columns={["Variable", "Value"]}
            items={entries}
            keyFn={([k]) => k}
            renderRow={([k, v]) => (
              <>
                <td className="p-3 font-mono text-xs">{k}</td>
                <td className="p-3 font-mono text-xs break-all">{v}</td>
              </>
            )}
          />
        )
      ) : (
        <div className="space-y-3">
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-max whitespace-nowrap">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="p-3 text-left text-sm font-medium">Variable</th>
                  <th className="p-3 text-left text-sm font-medium">Value</th>
                  <th className="p-3" />
                </tr>
              </thead>
              <tbody>
                {draftEntries.map(([k, v]) => (
                  <tr key={k} className="border-b last:border-b-0">
                    <td className="p-3 font-mono text-xs">{k}</td>
                    <td className="p-2">
                      <input
                        type="text"
                        value={v}
                        onChange={(e) => setDraft((prev) => ({ ...prev, [k]: e.target.value }))}
                        className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                      />
                    </td>
                    <td className="p-2">
                      <button
                        type="button"
                        onClick={() => removeRow(k)}
                        className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-red-600"
                        title="Remove"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </td>
                  </tr>
                ))}
                <tr>
                  <td className="p-2">
                    <input
                      type="text"
                      value={newKey}
                      onChange={(e) => setNewKey(e.target.value)}
                      placeholder="NEW_VAR"
                      className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </td>
                  <td className="p-2">
                    <input
                      type="text"
                      value={newVal}
                      onChange={(e) => setNewVal(e.target.value)}
                      placeholder="value"
                      onKeyDown={(e) => { if (e.key === "Enter") addRow(); }}
                      className="w-full rounded border bg-background px-2 py-1 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                  </td>
                  <td className="p-2">
                    <button
                      type="button"
                      onClick={addRow}
                      disabled={!newKey.trim()}
                      className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-40"
                      title="Add"
                    >
                      <Plus className="h-3.5 w-3.5" />
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void save()}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {saving && <Spinner />}
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-xs font-medium disabled:opacity-50"
            >
              <X className="h-3 w-3" />
              Cancel
            </button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}

interface ServiceResourceShape {
  limits?: { nanoCPUs?: number; memoryBytes?: number; pids?: number };
  reservations?: { nanoCPUs?: number; memoryBytes?: number };
}

function ResourcesEditor({
  serviceId,
  resources,
  onSaved,
}: {
  serviceId: string;
  resources: Record<string, unknown>;
  onSaved: (updated: Record<string, unknown>) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  const typed = resources as ServiceResourceShape;

  const [limitCpu, setLimitCpu] = useState("");
  const [limitMem, setLimitMem] = useState("");
  const [resCpu, setResCpu] = useState("");
  const [resMem, setResMem] = useState("");

  function openEdit() {
    setLimitCpu(typed.limits?.nanoCPUs != null ? String(typed.limits.nanoCPUs / 1e9) : "");
    setLimitMem(typed.limits?.memoryBytes != null ? String(typed.limits.memoryBytes) : "");
    setResCpu(typed.reservations?.nanoCPUs != null ? String(typed.reservations.nanoCPUs / 1e9) : "");
    setResMem(typed.reservations?.memoryBytes != null ? String(typed.reservations.memoryBytes) : "");
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(null);
  }

  async function save() {
    const patch: ServiceResourceShape = {};
    if (limitCpu || limitMem) {
      patch.limits = {};
      if (limitCpu) patch.limits.nanoCPUs = Math.round(parseFloat(limitCpu) * 1e9);
      if (limitMem) patch.limits.memoryBytes = parseInt(limitMem, 10);
    }
    if (resCpu || resMem) {
      patch.reservations = {};
      if (resCpu) patch.reservations.nanoCPUs = Math.round(parseFloat(resCpu) * 1e9);
      if (resMem) patch.reservations.memoryBytes = parseInt(resMem, 10);
    }
    setSaving(true);
    setSaveError(null);
    try {
      const updated = await api.patchServiceResources(serviceId, patch);
      onSaved(updated);
      setEditing(false);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Save failed");
    } finally {
      setSaving(false);
    }
  }

  const hasResources =
    typed.limits?.nanoCPUs ||
    typed.limits?.memoryBytes ||
    typed.reservations?.nanoCPUs ||
    typed.reservations?.memoryBytes;

  const controls = !editing ? (
    <button
      type="button"
      onClick={openEdit}
      className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs font-medium hover:bg-accent"
    >
      <Pencil className="h-3 w-3" />
      Edit
    </button>
  ) : null;

  return (
    <CollapsibleSection
      title="Resource Limits"
      defaultOpen={false}
      controls={controls}
    >
      {!editing ? (
        !hasResources ? (
          <p className="text-sm text-muted-foreground">No resource limits configured.</p>
        ) : (
          <div className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
            {typed.limits?.nanoCPUs != null && (
              <div>
                <div className="text-xs text-muted-foreground">CPU Limit</div>
                <div className="font-mono">{(typed.limits.nanoCPUs / 1e9).toFixed(2)} cores</div>
              </div>
            )}
            {typed.limits?.memoryBytes != null && (
              <div>
                <div className="text-xs text-muted-foreground">Memory Limit</div>
                <div className="font-mono">{formatBytes(typed.limits.memoryBytes)}</div>
              </div>
            )}
            {typed.reservations?.nanoCPUs != null && (
              <div>
                <div className="text-xs text-muted-foreground">CPU Reserved</div>
                <div className="font-mono">{(typed.reservations.nanoCPUs / 1e9).toFixed(2)} cores</div>
              </div>
            )}
            {typed.reservations?.memoryBytes != null && (
              <div>
                <div className="text-xs text-muted-foreground">Memory Reserved</div>
                <div className="font-mono">{formatBytes(typed.reservations.memoryBytes)}</div>
              </div>
            )}
          </div>
        )
      ) : (
        <div className="space-y-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Limits</h4>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">CPU (cores)</span>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={limitCpu}
                  onChange={(e) => setLimitCpu(e.target.value)}
                  placeholder="e.g. 0.5"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">Memory (bytes)</span>
                <input
                  type="number"
                  min="0"
                  value={limitMem}
                  onChange={(e) => setLimitMem(e.target.value)}
                  placeholder="e.g. 536870912"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
            </div>
            <div className="space-y-2 rounded-lg border p-3">
              <h4 className="text-xs font-medium text-muted-foreground uppercase">Reservations</h4>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">CPU (cores)</span>
                <input
                  type="number"
                  step="0.01"
                  min="0"
                  value={resCpu}
                  onChange={(e) => setResCpu(e.target.value)}
                  placeholder="e.g. 0.25"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
              <label className="flex flex-col gap-1">
                <span className="text-xs text-muted-foreground">Memory (bytes)</span>
                <input
                  type="number"
                  min="0"
                  value={resMem}
                  onChange={(e) => setResMem(e.target.value)}
                  placeholder="e.g. 268435456"
                  className="rounded border bg-background px-2 py-1 font-mono text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                />
              </label>
            </div>
          </div>
          {saveError && <p className="text-xs text-red-600 dark:text-red-400">{saveError}</p>}
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => void save()}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground disabled:opacity-50"
            >
              {saving && <Spinner />}
              Save
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={saving}
              className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-xs font-medium disabled:opacity-50"
            >
              <X className="h-3 w-3" />
              Cancel
            </button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}

function formatCpu(nanoCpus: number): string {
  return `${(nanoCpus / 1_000_000_000).toFixed(2)} cores`;
}

function cpuThresholds(service: Service): Threshold[] {
  const res = service.Spec.TaskTemplate.Resources;
  if (!res) {
    return [];
  }
  const out: Threshold[] = [];
  if (res.Reservations?.NanoCPUs) {
    out.push({
      label: "Reserved",
      value: (res.Reservations.NanoCPUs / 1e9) * 100,
      color: "#3b82f6",
      dash: [12, 6],
    });
  }
  if (res.Limits?.NanoCPUs) {
    out.push({
      label: "Limit",
      value: (res.Limits.NanoCPUs / 1e9) * 100,
      color: "#ef4444",
      dash: [12, 6],
    });
  }
  return out;
}

function memoryThresholds(service: Service): Threshold[] {
  const res = service.Spec.TaskTemplate.Resources;
  if (!res) {
    return [];
  }
  const out: Threshold[] = [];
  if (res.Reservations?.MemoryBytes) {
    out.push({
      label: "Reserved",
      value: res.Reservations.MemoryBytes,
      color: "#3b82f6",
      dash: [12, 6],
    });
  }
  if (res.Limits?.MemoryBytes) {
    out.push({
      label: "Limit",
      value: res.Limits.MemoryBytes,
      color: "#ef4444",
      dash: [12, 6],
    });
  }
  return out;
}
