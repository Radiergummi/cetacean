import { useParams, Link } from "react-router-dom";
import { useState, useEffect, useMemo } from "react";
import { api } from "../api/client";
import type { Service, Task } from "../api/types";
import MetricsPanel from "../components/MetricsPanel";
import type { Threshold } from "../components/TimeSeriesChart";
import InfoCard from "../components/InfoCard";
import TaskStatusBadge from "../components/TaskStatusBadge";
import TaskStateFilter from "../components/TaskStateFilter";
import LogViewer from "../components/LogViewer";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import { statusBorder } from "../lib/statusBorder";
import TimeAgo, { timeAgo } from "../components/TimeAgo";

export default function ServiceDetail() {
  const { id } = useParams<{ id: string }>();
  const [service, setService] = useState<Service | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [stateFilter, setStateFilter] = useState<string | null>(null);

  useEffect(() => {
    if (id) {
      api.service(id).then(setService);
      api
        .serviceTasks(id)
        .then(setTasks)
        .catch(() => {});
    }
  }, [id]);

  const filteredTasks = useMemo(() => {
    const filtered = stateFilter ? tasks.filter((t) => t.Status.State === stateFilter) : tasks;
    return [...filtered].sort(
      (a, b) => new Date(b.Status.Timestamp).getTime() - new Date(a.Status.Timestamp).getTime(),
    );
  }, [tasks, stateFilter]);

  if (!service) return <LoadingDetail />;

  const name = service.Spec.Name || service.ID;
  const cs = service.Spec.TaskTemplate.ContainerSpec;
  const tt = service.Spec.TaskTemplate;
  const labels = service.Spec.Labels;
  const nonStackLabels = Object.entries(labels).filter(([k]) => !k.startsWith("com.docker.stack."));

  return (
    <div>
      <PageHeader
        title={name}
        breadcrumbs={[{ label: "Services", to: "/services" }, { label: name }]}
      />

      {/* Overview cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
        <InfoCard label="Image" value={cs.Image.split("@")[0]} />
        <InfoCard
          label="Mode"
          value={
            service.Spec.Mode.Replicated
              ? `replicated (${service.Spec.Mode.Replicated.Replicas})`
              : "global"
          }
        />
        <InfoCard
          label="Update Status"
          value={
            service.UpdateStatus
              ? [
                  service.UpdateStatus.State,
                  service.UpdateStatus.Message && `— ${service.UpdateStatus.Message}`,
                  service.UpdateStatus.StartedAt &&
                    `started ${timeAgo(service.UpdateStatus.StartedAt)}`,
                  service.UpdateStatus.CompletedAt &&
                    `completed ${timeAgo(service.UpdateStatus.CompletedAt)}`,
                ]
                  .filter(Boolean)
                  .join(" ")
              : undefined
          }
        />
        <InfoCard
          label="Stack"
          value={labels["com.docker.stack.namespace"]}
          href={
            labels["com.docker.stack.namespace"]
              ? `/stacks/${labels["com.docker.stack.namespace"]}`
              : undefined
          }
        />
        {service.CreatedAt && <InfoCard label="Created" value={timeAgo(service.CreatedAt)} />}
        {service.UpdatedAt && <InfoCard label="Updated" value={timeAgo(service.UpdatedAt)} />}
      </div>

      {/* Container configuration */}
      <Section title="Container Configuration">
        <KVTable
          rows={[
            ["Image", cs.Image],
            cs.Command && ["Command", cs.Command.join(" ")],
            cs.Args && ["Args", cs.Args.join(" ")],
            cs.User && ["User", cs.User],
            cs.Dir && ["Working Dir", cs.Dir],
            cs.Hostname && ["Hostname", cs.Hostname],
            cs.StopSignal && ["Stop Signal", cs.StopSignal],
            cs.StopGracePeriod != null && ["Stop Grace Period", formatNs(cs.StopGracePeriod)],
            cs.Init != null && ["Init", cs.Init ? "yes" : "no"],
            cs.ReadOnly && ["Read Only Root FS", "yes"],
          ]}
        />
      </Section>

      {/* Environment variables */}
      <Section title="Environment Variables">
        {cs.Env && cs.Env.length > 0 ? (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Variable</th>
                  <th className="text-left p-3 text-sm font-medium">Value</th>
                </tr>
              </thead>
              <tbody>
                {cs.Env.map((env) => {
                  const eqIdx = env.indexOf("=");
                  const key = eqIdx >= 0 ? env.slice(0, eqIdx) : env;
                  const val = eqIdx >= 0 ? env.slice(eqIdx + 1) : "";
                  return (
                    <tr key={env} className="border-b">
                      <td className="p-3 text-sm font-mono text-xs">{key}</td>
                      <td className="p-3 text-sm font-mono text-xs break-all">{val}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <SectionEmpty />
        )}
      </Section>

      {/* Healthcheck */}
      <Section title="Healthcheck">
        {cs.Healthcheck ? (
          <KVTable
            rows={[
              cs.Healthcheck.Test && ["Test", cs.Healthcheck.Test.join(" ")],
              cs.Healthcheck.Interval != null && ["Interval", formatNs(cs.Healthcheck.Interval)],
              cs.Healthcheck.Timeout != null && ["Timeout", formatNs(cs.Healthcheck.Timeout)],
              cs.Healthcheck.Retries != null && ["Retries", String(cs.Healthcheck.Retries)],
              cs.Healthcheck.StartPeriod != null && [
                "Start Period",
                formatNs(cs.Healthcheck.StartPeriod),
              ],
            ]}
          />
        ) : (
          <SectionEmpty />
        )}
      </Section>

      {/* Labels */}
      <Section title="Labels">
        {nonStackLabels.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {nonStackLabels.map(([k, v]) => (
              <span
                key={k}
                className="inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-mono"
              >
                <span className="text-muted-foreground">{k}=</span>
                {v}
              </span>
            ))}
          </div>
        ) : (
          <SectionEmpty />
        )}
      </Section>

      {/* Ports */}
      <Section title="Ports">
        {service.Endpoint?.Ports && service.Endpoint.Ports.length > 0 ? (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Published</th>
                  <th className="text-left p-3 text-sm font-medium">Target</th>
                  <th className="text-left p-3 text-sm font-medium">Protocol</th>
                  <th className="text-left p-3 text-sm font-medium">Mode</th>
                </tr>
              </thead>
              <tbody>
                {service.Endpoint.Ports.map((port, i) => (
                  <tr key={i} className="border-b">
                    <td className="p-3 text-sm">{port.PublishedPort}</td>
                    <td className="p-3 text-sm">{port.TargetPort}</td>
                    <td className="p-3 text-sm">{port.Protocol}</td>
                    <td className="p-3 text-sm">{port.PublishMode}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <SectionEmpty />
        )}
      </Section>

      {/* Mounts */}
      <Section title="Mounts">
        {cs.Mounts && cs.Mounts.length > 0 ? (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Type</th>
                  <th className="text-left p-3 text-sm font-medium">Source</th>
                  <th className="text-left p-3 text-sm font-medium">Target</th>
                  <th className="text-left p-3 text-sm font-medium">Read Only</th>
                </tr>
              </thead>
              <tbody>
                {cs.Mounts.map((m, i) => (
                  <tr key={i} className="border-b">
                    <td className="p-3 text-sm">{m.Type}</td>
                    <td className="p-3 text-sm font-mono text-xs">{m.Source || "\u2014"}</td>
                    <td className="p-3 text-sm font-mono text-xs">{m.Target}</td>
                    <td className="p-3 text-sm">{m.ReadOnly ? "yes" : "no"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <SectionEmpty />
        )}
      </Section>

      {/* Configs */}
      <Section title="Configs">
        {cs.Configs && cs.Configs.length > 0 ? (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Name</th>
                  <th className="text-left p-3 text-sm font-medium">Target</th>
                </tr>
              </thead>
              <tbody>
                {cs.Configs.map((cfg) => (
                  <tr key={cfg.ConfigID} className="border-b">
                    <td className="p-3 text-sm">{cfg.ConfigName}</td>
                    <td className="p-3 text-sm font-mono text-xs">{cfg.File?.Name || "\u2014"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <SectionEmpty />
        )}
      </Section>

      {/* Secrets */}
      <Section title="Secrets">
        {cs.Secrets && cs.Secrets.length > 0 ? (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead className="sticky top-0 z-10 bg-background">
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Name</th>
                  <th className="text-left p-3 text-sm font-medium">Target</th>
                </tr>
              </thead>
              <tbody>
                {cs.Secrets.map((s) => (
                  <tr key={s.SecretID} className="border-b">
                    <td className="p-3 text-sm">{s.SecretName}</td>
                    <td className="p-3 text-sm font-mono text-xs">{s.File?.Name || "\u2014"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <SectionEmpty />
        )}
      </Section>

      {/* Deploy: Resources, Placement, Restart, Update, Rollback */}
      <Section title="Deploy Configuration">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {/* Resources */}
          {tt.Resources && (
            <div>
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-2">
                Resources
              </h3>
              <KVTable
                rows={[
                  tt.Resources.Limits?.NanoCPUs && [
                    "CPU Limit",
                    formatCpu(tt.Resources.Limits.NanoCPUs),
                  ],
                  tt.Resources.Limits?.MemoryBytes && [
                    "Memory Limit",
                    formatBytes(tt.Resources.Limits.MemoryBytes),
                  ],
                  tt.Resources.Limits?.Pids && ["PID Limit", String(tt.Resources.Limits.Pids)],
                  tt.Resources.Reservations?.NanoCPUs && [
                    "CPU Reservation",
                    formatCpu(tt.Resources.Reservations.NanoCPUs),
                  ],
                  tt.Resources.Reservations?.MemoryBytes && [
                    "Memory Reservation",
                    formatBytes(tt.Resources.Reservations.MemoryBytes),
                  ],
                ]}
              />
            </div>
          )}

          {/* Restart policy */}
          {tt.RestartPolicy && (
            <div>
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-2">
                Restart Policy
              </h3>
              <KVTable
                rows={[
                  tt.RestartPolicy.Condition && ["Condition", tt.RestartPolicy.Condition],
                  tt.RestartPolicy.Delay != null && ["Delay", formatNs(tt.RestartPolicy.Delay)],
                  tt.RestartPolicy.MaxAttempts != null && [
                    "Max Attempts",
                    String(tt.RestartPolicy.MaxAttempts),
                  ],
                  tt.RestartPolicy.Window != null && ["Window", formatNs(tt.RestartPolicy.Window)],
                ]}
              />
            </div>
          )}

          {/* Placement */}
          {tt.Placement && (
            <div>
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-2">
                Placement
              </h3>
              <KVTable
                rows={[
                  tt.Placement.Constraints &&
                    tt.Placement.Constraints.length > 0 && [
                      "Constraints",
                      tt.Placement.Constraints.join(", "),
                    ],
                  tt.Placement.MaxReplicas && [
                    "Max Replicas per Node",
                    String(tt.Placement.MaxReplicas),
                  ],
                  tt.Placement.Preferences &&
                    tt.Placement.Preferences.length > 0 && [
                      "Preferences",
                      tt.Placement.Preferences.map((p) => p.Spread?.SpreadDescriptor)
                        .filter(Boolean)
                        .join(", "),
                    ],
                ]}
              />
            </div>
          )}

          {/* Log driver */}
          {tt.LogDriver && (
            <div>
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-2">
                Log Driver
              </h3>
              <KVTable
                rows={[
                  ["Driver", tt.LogDriver.Name],
                  ...(tt.LogDriver.Options
                    ? Object.entries(tt.LogDriver.Options).map(
                        ([k, v]) => [k, v] as [string, string],
                      )
                    : []),
                ]}
              />
            </div>
          )}

          {/* Update config */}
          {service.Spec.UpdateConfig && (
            <div>
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-2">
                Update Config
              </h3>
              <KVTable rows={updateConfigRows(service.Spec.UpdateConfig)} />
            </div>
          )}

          {/* Rollback config */}
          {service.Spec.RollbackConfig && (
            <div>
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-2">
                Rollback Config
              </h3>
              <KVTable rows={updateConfigRows(service.Spec.RollbackConfig)} />
            </div>
          )}
        </div>
      </Section>

      {/* Tasks */}
      <Section title="Tasks">
        {tasks.length > 0 ? (
          <>
            <div className="flex items-center justify-end mb-3 -mt-1">
              <TaskStateFilter tasks={tasks} active={stateFilter} onChange={setStateFilter} />
            </div>
            <div className="overflow-x-auto rounded-lg border">
              <table className="w-full">
                <thead className="sticky top-0 z-10 bg-background">
                  <tr className="border-b bg-muted/50">
                    <th className="text-left p-3 text-sm font-medium">ID</th>
                    <th className="text-left p-3 text-sm font-medium">Slot</th>
                    <th className="text-left p-3 text-sm font-medium">State</th>
                    <th className="text-left p-3 text-sm font-medium">Node</th>
                    <th className="text-left p-3 text-sm font-medium">Desired</th>
                    <th className="text-left p-3 text-sm font-medium">Error</th>
                    <th className="text-left p-3 text-sm font-medium">Timestamp</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredTasks.map((task) => {
                    const exitCode = task.Status.ContainerStatus?.ExitCode;
                    const errorMsg =
                      task.Status.Err || (exitCode && exitCode !== 0 ? `exit ${exitCode}` : "");
                    return (
                      <tr key={task.ID} className={`border-b ${statusBorder(task.Status.State)}`}>
                        <td className="p-3 text-sm font-mono text-xs">
                          <Link to={`/tasks/${task.ID}`} className="text-link hover:underline">
                            {task.ID.slice(0, 12)}
                          </Link>
                        </td>
                        <td className="p-3 text-sm">{task.Slot}</td>
                        <td className="p-3 text-sm">
                          <TaskStatusBadge state={task.Status.State} />
                        </td>
                        <td className="p-3 text-sm">
                          <Link to={`/nodes/${task.NodeID}`} className="text-link hover:underline">
                            {task.NodeID.slice(0, 12)}
                          </Link>
                        </td>
                        <td className="p-3 text-sm">{task.DesiredState}</td>
                        <td className="p-3 text-sm text-red-600 dark:text-red-400">{errorMsg}</td>
                        <td className="p-3 text-sm text-muted-foreground">
                          {task.Status.Timestamp ? (
                            <TimeAgo date={task.Status.Timestamp} />
                          ) : (
                            "\u2014"
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </>
        ) : (
          <SectionEmpty />
        )}
      </Section>

      <div className="mb-6">
        <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
          Logs
        </h2>
        <LogViewer serviceId={id!} />
      </div>

      <MetricsPanel
        charts={[
          {
            title: "CPU Usage",
            query: `sum(rate(container_cpu_usage_seconds_total{id=~"/docker/.+"}[5m]))`,
            unit: "cores",
            thresholds: cpuThresholds(service),
          },
          {
            title: "Memory Usage",
            query: `sum(container_memory_usage_bytes{id=~"/docker/.+"})`,
            unit: "bytes",
            thresholds: memoryThresholds(service),
          },
        ]}
      />
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="mb-6">
      <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
        {title}
      </h2>
      {children}
    </div>
  );
}

function SectionEmpty() {
  return <p className="text-sm text-muted-foreground">None</p>;
}

function KVTable({ rows }: { rows: (false | undefined | null | 0 | "" | [string, string])[] }) {
  const valid = rows.filter((r): r is [string, string] => !!r && !!r[1]);
  if (valid.length === 0) return null;
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full">
        <tbody>
          {valid.map(([k, v]) => (
            <tr key={k} className="border-b last:border-b-0">
              <td className="p-3 text-sm font-medium text-muted-foreground w-1/3">{k}</td>
              <td className="p-3 text-sm font-mono text-xs break-all">{v}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
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

function formatNs(ns: number): string {
  if (ns >= 60_000_000_000) return `${Math.round(ns / 60_000_000_000)}m`;
  if (ns >= 1_000_000_000) return `${Math.round(ns / 1_000_000_000)}s`;
  if (ns >= 1_000_000) return `${Math.round(ns / 1_000_000)}ms`;
  return `${ns}ns`;
}

function formatCpu(nanoCpus: number): string {
  return `${(nanoCpus / 1_000_000_000).toFixed(2)} cores`;
}

function formatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(0)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${bytes} B`;
}

function cpuThresholds(service: Service): Threshold[] {
  const res = service.Spec.TaskTemplate.Resources;
  if (!res) return [];
  const out: Threshold[] = [];
  if (res.Reservations?.NanoCPUs) {
    out.push({
      label: "Reserved",
      value: res.Reservations.NanoCPUs / 1e9,
      color: "#3b82f6",
      dash: [6, 4],
    });
  }
  if (res.Limits?.NanoCPUs) {
    out.push({
      label: "Limit",
      value: res.Limits.NanoCPUs / 1e9,
      color: "#ef4444",
      dash: [6, 4],
    });
  }
  return out;
}

function memoryThresholds(service: Service): Threshold[] {
  const res = service.Spec.TaskTemplate.Resources;
  if (!res) return [];
  const out: Threshold[] = [];
  if (res.Reservations?.MemoryBytes) {
    out.push({
      label: "Reserved",
      value: res.Reservations.MemoryBytes,
      color: "#3b82f6",
      dash: [6, 4],
    });
  }
  if (res.Limits?.MemoryBytes) {
    out.push({
      label: "Limit",
      value: res.Limits.MemoryBytes,
      color: "#ef4444",
      dash: [6, 4],
    });
  }
  return out;
}
