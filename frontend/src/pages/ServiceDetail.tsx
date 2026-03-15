import { Globe, Shuffle } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { HistoryEntry, Service, Task } from "../api/types";
import ActivityFeed from "../components/ActivityFeed";
import CollapsibleSection from "../components/CollapsibleSection";
import { ContainerImage, KVTable, MetadataGrid, ResourceLink, Timestamp } from "../components/data";
import { formatNs } from "../lib/formatNs";
import { useTaskMetrics } from "../hooks/useTaskMetrics";
import { escapePromQL } from "../lib/utils";
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
import { formatBytes } from "../lib/formatBytes";

export default function ServiceDetail() {
  const { id } = useParams<{ id: string }>();
  const [service, setService] = useState<Service | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
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
      .then(setService)
      .catch(() => setError(true));
    api
      .serviceTasks(id)
      .then(setTasks)
      .catch(() => {});
    api
      .history({ resourceId: id, limit: 10 })
      .then(setHistory)
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
      api.metricsQuery(`sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${escaped}"}[5m])) * 100`),
      api.metricsQuery(`sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${escaped}"})`),
    ]).then(([cpuResp, memResp]) => {
      if (cancelled) return;
      const cpuVal = cpuResp.data?.result?.[0]?.value?.[1];
      const memVal = memResp.data?.result?.[0]?.value?.[1];
      if (cpuVal != null) setCpuActual(Number(cpuVal));
      if (memVal != null) setMemActual(Number(memVal));
    }).catch(() => {});

    return () => { cancelled = true; };
  }, [serviceName, hasCadvisor]);

  if (error) {
    return <FetchError message="Failed to load service" />;
  }

  if (!service) {
    return <LoadingDetail />;
  }

  const name = service.Spec.Name || service.ID;
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

  const metricsCharts = useMemo(() => [
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
  ], [name, service]);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={<ResourceName name={name} />}
        breadcrumbs={[
          { label: "Services", to: "/services" },
          { label: <ResourceName name={name} /> },
        ]}
      />

      {/* Overview cards */}
      <MetadataGrid>
        <ContainerImage image={containerSpec.Image} />
        <ReplicaCard service={service} tasks={tasks} />
        <ServiceStatusCard service={service} />
        <ResourceLink
          label="Stack"
          name={labels["com.docker.stack.namespace"]}
          to={`/stacks/${labels["com.docker.stack.namespace"]}`}
        />
        <Timestamp label="Created" date={service.CreatedAt} />
        <Timestamp label="Updated" date={service.UpdatedAt} />
      </MetadataGrid>

      {/* Tasks */}
      <TasksTable tasks={tasks} variant="service" metrics={hasCadvisor ? taskMetrics : undefined} />

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

      {/* Container configuration */}
      {hasContainerConfig && (
        <CollapsibleSection title="Container Configuration" defaultOpen={false}>
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
      {containerSpec.Env && containerSpec.Env.length > 0 && (
        <CollapsibleSection title="Environment Variables" defaultOpen={false}>
          <SimpleTable
            columns={["Variable", "Value"]}
            items={containerSpec.Env}
            keyFn={(env) => env}
            renderRow={(env) => {
              const eqIdx = env.indexOf("=");
              const key = eqIdx >= 0 ? env.slice(0, eqIdx) : env;
              const val = eqIdx >= 0 ? env.slice(eqIdx + 1) : "";
              return (
                <>
                  <td className="p-3 font-mono text-xs">{key}</td>
                  <td className="p-3 font-mono text-xs break-all">{val}</td>
                </>
              );
            }}
          />
        </CollapsibleSection>
      )}

      {/* Healthcheck */}
      {containerSpec.Healthcheck && (
        <CollapsibleSection title="Healthcheck" defaultOpen={false}>
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
        <CollapsibleSection title="Labels" defaultOpen={false}>
          <div className="flex flex-wrap gap-2">
            {nonStackLabels.map(([key, value]) => (
              <span
                key={key}
                className="inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-mono"
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
        <CollapsibleSection title="Ports" defaultOpen={false}>
          <div className="flex flex-wrap gap-2">
            {service.Endpoint.Ports.map(
              ({ Protocol, PublishMode, PublishedPort, TargetPort }, index) => (
                <span
                  key={index}
                  className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 text-sm font-mono"
                >
                  <span className="font-semibold">{PublishedPort}</span>
                  <span className="text-muted-foreground">&rarr;</span>
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
        <CollapsibleSection title="Mounts" defaultOpen={false}>
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
                    <Link to={`/volumes/${Source}`} className="text-link hover:underline">
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
        <CollapsibleSection title="Networks" defaultOpen={false}>
          <SimpleTable
            columns={["Network", "Virtual IP", "Aliases"]}
            items={taskTemplate.Networks}
            keyFn={({ Target }) => Target}
            renderRow={({ Aliases, Target }) => {
              const vip = service.Endpoint?.VirtualIPs?.find((v) => v.NetworkID === Target);
              return (
                <>
                  <td className="p-3 text-sm">
                    <Link to={`/networks/${Target}`} className="text-link hover:underline">
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
        <CollapsibleSection title="Configs" defaultOpen={false}>
          <SimpleTable
            columns={["Name", "Target"]}
            items={containerSpec.Configs}
            keyFn={({ ConfigID }) => ConfigID}
            renderRow={({ ConfigID, ConfigName, File }) => (
              <>
                <td className="p-3 text-sm">
                  <Link to={`/configs/${ConfigID}`} className="text-link hover:underline">
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
        <CollapsibleSection title="Secrets" defaultOpen={false}>
          <SimpleTable
            columns={["Name", "Target"]}
            items={containerSpec.Secrets}
            keyFn={({ SecretID }) => SecretID}
            renderRow={({ File, SecretID, SecretName }) => (
              <>
                <td className="p-3 text-sm">
                  <Link to={`/secrets/${SecretID}`} className="text-link hover:underline">
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
      <CollapsibleSection title="Deploy Configuration" defaultOpen={false}>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {taskTemplate.Resources && (
            <div className="rounded-lg border p-4 flex flex-col gap-3">
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Resources
              </h3>

              <ResourcesPanel resources={taskTemplate.Resources} />
            </div>
          )}

          {taskTemplate.Placement && (
            <div className="rounded-lg border p-4 flex flex-col gap-3">
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Placement
              </h3>

              <PlacementPanel placement={taskTemplate.Placement} />
            </div>
          )}

          {taskTemplate.RestartPolicy && (
            <div className="flex flex-col gap-2">
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
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
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
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
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Update Config
              </h3>
              <KVTable rows={updateConfigRows(service.Spec.UpdateConfig)} />
            </div>
          )}

          {service.Spec.RollbackConfig && (
            <div className="flex flex-col gap-2">
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Rollback Config
              </h3>
              <KVTable rows={updateConfigRows(service.Spec.RollbackConfig)} />
            </div>
          )}

          {service.Spec.EndpointSpec?.Mode && (
            <div className="rounded-lg border p-4 flex flex-col gap-3">
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
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
        <LogViewer serviceId={id!} header="Logs" />
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
            <span className="text-xs text-muted-foreground truncate" title={msg}>
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

function ReplicaCard({ service, tasks }: { service: Service; tasks: Task[] }) {
  const replicated = service.Spec.Mode.Replicated;
  if (!replicated) {
    return <InfoCard label="Mode" value="global" />;
  }

  const desired = replicated.Replicas ?? 0;
  const running = tasks.filter((t) => t.Status.State === "running").length;
  const healthy = running >= desired;
  const value = (
    <>
      <span className="tabular-nums">
        <span className="text-2xl font-bold">{running}</span>
        <span className="text-muted-foreground font-normal text-lg">/{desired}</span>
      </span>

      {!healthy && (
        <div className="text-xs text-red-600 dark:text-red-400 mt-1">
          {desired - running} replica{desired - running !== 1 ? "s" : ""} not running
        </div>
      )}
    </>
  );

  return <InfoCard label="Replicas" value={value} />;
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
                className="inline-flex items-center rounded-md border px-2.5 py-1 text-xs font-mono"
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
        <div className="h-2 rounded-full bg-muted overflow-hidden">
          <div className="h-full rounded-full bg-blue-500" style={{ width: `${reservedPct}%` }} />
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
