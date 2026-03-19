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
import { MetricsPanel, type Threshold } from "../components/metrics";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import {
  DeploymentChanges,
  EnvEditor,
  PlacementPanel,
  ReplicaCard,
  ResourcesEditor,
  ServiceActions,
  type ServiceResourceShape,
} from "../components/service-detail";
import SimpleTable from "../components/SimpleTable";
import TasksTable from "../components/TasksTable";
import { useMonitoringStatus } from "../hooks/useMonitoringStatus";
import { useResourceStream } from "../hooks/useResourceStream";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { getSemanticChartColor } from "../lib/chartColors";
import { formatDuration, formatRelativeDate } from "../lib/format";
import { escapePromQL } from "../lib/utils";
import { ArrowRight, Globe, Shuffle } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";

export default function ServiceDetail() {
  const { id } = useParams<{ id: string }>();
  const [service, setService] = useState<Service | null>(null);
  const [changes, setChanges] = useState<SpecChange[]>([]);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [envVars, setEnvVars] = useState<Record<string, string> | null>(null);
  const [serviceResources, setServiceResources] = useState<ServiceResourceShape | null>(null);
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
      .then((response) => {
        setService(response.service);
        setChanges(response.changes ?? []);
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
      .then((result) => {
        const map: Record<string, string> = {};

        for (const network of result.items) {
          map[network.Id] = network.Name;
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
    if (!serviceName || !hasCadvisor) {
      return;
    }
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
        if (cancelled) {
          return;
        }
        const cpuVal = cpuResp.data?.result?.[0]?.value?.[1];
        const memVal = memResp.data?.result?.[0]?.value?.[1];
        if (cpuVal != null) {
          setCpuActual(Number(cpuVal));
        }
        if (memVal != null) {
          setMemActual(Number(memVal));
        }
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
              query: `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${escapePromQL(
                name,
              )}"}[5m])) * 100`,
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
              color: getSemanticChartColor("memory"),
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

  const runningTasks = tasks?.filter(({ Status }) => Status?.State === "running").length ?? 0;
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
        title={
          <ResourceName
            name={name}
            direction="column"
          />
        }
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: <ResourceName name={name} /> },
        ]}
        actions={
          <ServiceActions
            service={service}
            serviceId={id!}
          />
        }
      />

      {/* Overview cards */}
      <MetadataGrid>
        <ContainerImage
          image={containerSpec.Image}
          serviceId={id}
        />
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
                formatDuration(containerSpec.StopGracePeriod),
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
                formatDuration(containerSpec.Healthcheck.Interval),
              ],
              containerSpec.Healthcheck.Timeout != null && [
                "Timeout",
                formatDuration(containerSpec.Healthcheck.Timeout),
              ],
              containerSpec.Healthcheck.Retries != null && [
                "Retries",
                String(containerSpec.Healthcheck.Retries),
              ],
              containerSpec.Healthcheck.StartPeriod != null && [
                "Start Period",
                formatDuration(containerSpec.Healthcheck.StartPeriod),
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
            keyFn={(_, index) => index}
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
              const vip = service.Endpoint?.VirtualIPs?.find(
                ({ NetworkID }) => NetworkID === Target,
              );
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
          {serviceResources !== null && (
            <div className="flex flex-col gap-3 rounded-lg border p-4">
              <h3 className="text-xs font-medium tracking-wider text-muted-foreground uppercase">
                Resources
              </h3>
              <ResourcesEditor
                serviceId={id!}
                resources={serviceResources}
                onSaved={setServiceResources}
                pids={taskTemplate.Resources?.Limits?.Pids}
                allocation={{ cpuReserved, cpuLimit, cpuActual, memReserved, memLimit, memActual }}
              />
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
                    formatDuration(taskTemplate.RestartPolicy.Delay),
                  ],
                  taskTemplate.RestartPolicy.MaxAttempts != null && [
                    "Max Attempts",
                    String(taskTemplate.RestartPolicy.MaxAttempts),
                  ],
                  taskTemplate.RestartPolicy.Window != null && [
                    "Window",
                    formatDuration(taskTemplate.RestartPolicy.Window),
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

const statusLabels: Record<string, string> = {
  stable: "Stable",
  updating: "Updating",
  completed: "Stable",
  paused: "Paused",
  rollback_started: "Rolling back",
  rollback_paused: "Rollback paused",
  rollback_completed: "Rolled back",
};

function serviceStatus({ UpdateStatus }: Service): { label: string; state: string } {
  const state = UpdateStatus?.State;

  if (!state || state === "completed") {
    return { label: "Stable", state: "stable" };
  }

  return { label: statusLabels[state] || state, state };
}

function ServiceStatusCard({ service }: { service: Service }) {
  const { label, state } = serviceStatus(service);
  const ts = service.UpdateStatus?.CompletedAt || service.UpdateStatus?.StartedAt;
  const msg = service.UpdateStatus?.Message;

  return (
    <InfoCard
      label="Status"
      value={
        <div className="flex flex-col">
          <span
            data-state={state}
            className="text-base font-medium text-green-600 data-[state=paused]:text-amber-600 data-[state=rollback_completed]:text-amber-600 data-[state=rollback_paused]:text-amber-600 data-[state=rollback_started]:text-amber-600 data-[state=updating]:text-blue-600 dark:text-green-400 dark:data-[state=paused]:text-amber-400 dark:data-[state=rollback_completed]:text-amber-400 dark:data-[state=rollback_paused]:text-amber-400 dark:data-[state=rollback_started]:text-amber-400 dark:data-[state=updating]:text-blue-400"
          >
            {label}
          </span>
          {ts && <span className="text-xs text-muted-foreground">{formatRelativeDate(ts)}</span>}
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

function updateConfigRows({
  Delay,
  FailureAction,
  MaxFailureRatio,
  Monitor,
  Order,
  Parallelism,
}: UpdateConfigShape) {
  return [
    ["Parallelism", String(Parallelism)] as [string, string],
    Delay != null && (["Delay", formatDuration(Delay)] as [string, string]),
    FailureAction && (["Failure Action", FailureAction] as [string, string]),
    Monitor != null && (["Monitor", formatDuration(Monitor)] as [string, string]),
    MaxFailureRatio != null && (["Max Failure Ratio", String(MaxFailureRatio)] as [string, string]),
    Order && (["Order", Order] as [string, string]),
  ];
}

function MountTypeBadge({ type }: { type: string }) {
  return (
    <span
      data-type={type}
      className="inline-flex items-center rounded-md bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground data-[type=bind]:bg-amber-100 data-[type=bind]:text-amber-800 data-[type=tmpfs]:bg-purple-100 data-[type=tmpfs]:text-purple-800 data-[type=volume]:bg-blue-100 data-[type=volume]:text-blue-800 dark:data-[type=bind]:bg-amber-900/30 dark:data-[type=bind]:text-amber-300 dark:data-[type=tmpfs]:bg-purple-900/30 dark:data-[type=tmpfs]:text-purple-300 dark:data-[type=volume]:bg-blue-900/30 dark:data-[type=volume]:text-blue-300"
    >
      {type}
    </span>
  );
}

function cpuThresholds(service: Service): Threshold[] {
  const resources = service.Spec.TaskTemplate.Resources;

  if (!resources) {
    return [];
  }

  const out: Threshold[] = [];

  if (resources.Reservations?.NanoCPUs) {
    const value = (resources.Reservations.NanoCPUs / 1e9) * 100;

    out.push({
      label: "Reserved",
      value,
      color: getSemanticChartColor("reserved"),
      dash: [12, 6],
    });
  }

  if (resources.Limits?.NanoCPUs) {
    const value = (resources.Limits.NanoCPUs / 1e9) * 100;

    out.push({
      label: "Limit",
      value,
      color: getSemanticChartColor("critical"),
      dash: [12, 6],
    });
  }

  return out;
}

function memoryThresholds(service: Service): Threshold[] {
  const resources = service.Spec.TaskTemplate.Resources;

  if (!resources) {
    return [];
  }

  const out: Threshold[] = [];

  if (resources.Reservations?.MemoryBytes) {
    out.push({
      label: "Reserved",
      value: resources.Reservations.MemoryBytes,
      color: getSemanticChartColor("reserved"),
      dash: [12, 6],
    });
  }

  if (resources.Limits?.MemoryBytes) {
    out.push({
      label: "Limit",
      value: resources.Limits.MemoryBytes,
      color: getSemanticChartColor("critical"),
      dash: [12, 6],
    });
  }

  return out;
}
