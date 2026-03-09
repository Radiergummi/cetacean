import {ChevronRight} from "lucide-react";
import {useCallback, useEffect, useMemo, useState} from "react";
import {Link, useParams} from "react-router-dom";
import {api} from "../api/client";
import type {HistoryEntry, Service, Task} from "../api/types";
import {useSSE} from "../hooks/useSSE";
import {usePrometheusConfigured} from "../hooks/usePrometheusConfigured";
import ActivityFeed from "../components/ActivityFeed";
import ErrorBoundary from "../components/ErrorBoundary";
import InfoCard from "../components/InfoCard";
import {LoadingDetail} from "../components/LoadingSkeleton";
import LogViewer from "../components/LogViewer";
import MetricsPanel from "../components/MetricsPanel";
import PageHeader from "../components/PageHeader";
import TaskStateFilter from "../components/TaskStateFilter";
import TaskStatusBadge from "../components/TaskStatusBadge";
import TimeAgo, {timeAgo} from "../components/TimeAgo";
import type {Threshold} from "../components/TimeSeriesChart";
import {formatBytes} from "../lib/formatBytes";
import {imageRegistryUrl} from "../lib/imageUrl";
import {statusColor} from "../lib/statusColor";
import FetchError from "../components/FetchError";

export default function ServiceDetail() {
    const {id} = useParams<{ id: string }>();
    const [service, setService] = useState<Service | null>(null);
    const [tasks, setTasks] = useState<Task[]>([]);
    const [stateFilter, setStateFilter] = useState<string | null>(null);
    const [history, setHistory] = useState<HistoryEntry[]>([]);
    const hasPrometheus = usePrometheusConfigured();
    const [error, setError] = useState(false);

    const fetchData = useCallback(() => {
        if (!id) return;
        api.service(id).then(setService).catch(() => setError(true));
        api.serviceTasks(id).then(setTasks).catch(() => {});
        api.history({resourceId: id, limit: 10}).then(setHistory).catch(() => {});
    }, [id]);

    useEffect(fetchData, [fetchData]);

    useSSE(["service", "task"], (e) => {
        if (e.type === "service" && e.id === id) fetchData();
        if (e.type === "task") fetchData();
    });

    const filteredTasks = useMemo(() => {
        const filtered = stateFilter ? tasks.filter((t) => t.Status.State === stateFilter) : tasks;
        return [...filtered].sort(
            (a, b) => new Date(b.Status.Timestamp).getTime() - new Date(a.Status.Timestamp).getTime(),
        );
    }, [tasks, stateFilter]);

    if (error) return <FetchError message="Failed to load service" />;
    if (!service) {
        return <LoadingDetail/>;
    }

    const name = service.Spec.Name || service.ID;
    const cs = service.Spec.TaskTemplate.ContainerSpec;
    const tt = service.Spec.TaskTemplate;
    const labels = service.Spec.Labels;
    const nonStackLabels = Object.entries(labels).filter(([k]) => !k.startsWith("com.docker.stack."));

    const hasContainerConfig = cs.Command || cs.Args || cs.User || cs.Dir || cs.Hostname ||
        cs.StopSignal || cs.StopGracePeriod != null || cs.Init != null || cs.ReadOnly;

    return (
        <div className="flex flex-col gap-6">
            <PageHeader
                title={name}
                breadcrumbs={[{label: "Services", to: "/services"}, {label: name}]}
            />

            {/* Overview cards */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                <InfoCard label="Image" value={cs.Image.split("@")[0]} href={imageRegistryUrl(cs.Image) ?? undefined}/>
                <InfoCard
                    label="Mode"
                    value={
                        service.Spec.Mode.Replicated
                            ? <>replicated <span className="inline-flex items-center justify-center rounded-full bg-muted px-2 py-0.5 text-sm font-semibold tabular-nums">{service.Spec.Mode.Replicated.Replicas}</span></>
                            : "global"
                    }
                />
                <InfoCard
                    label="Update Status"
                    value={
                        service.UpdateStatus
                            ? `${service.UpdateStatus.State} ${timeAgo(
                                service.UpdateStatus.CompletedAt || service.UpdateStatus.StartedAt || "",
                            )}`
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
                {service.CreatedAt && <InfoCard label="Created" value={timeAgo(service.CreatedAt)}/>}
                {service.UpdatedAt && <InfoCard label="Updated" value={timeAgo(service.UpdatedAt)}/>}
            </div>

            {/* Tasks */}
            {tasks.length > 0 && (
                <Section
                    title="Tasks"
                    controls={<TaskStateFilter tasks={tasks} active={stateFilter} onChange={setStateFilter}/>}
                >
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
                                    task.Status.Err ||
                                    (
                                        exitCode && exitCode !== 0 ? `exit ${exitCode}` : ""
                                    );
                                return (
                                    <tr key={task.ID} className="border-b last:border-b-0">
                                        <td className="p-3 font-mono text-xs">
                                            <span className="inline-flex items-center gap-2">
                                                <span className={`shrink-0 w-2 h-2 rounded-full ${statusColor(task.Status.State)}`}/>
                                                <Link to={`/tasks/${task.ID}`} className="text-link hover:underline">
                                                    {task.ID.slice(0, 12)}
                                                </Link>
                                            </span>
                                        </td>
                                        <td className="p-3 text-sm">{task.Slot}</td>
                                        <td className="p-3 text-sm">
                                            <TaskStatusBadge state={task.Status.State}/>
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
                                                <TimeAgo date={task.Status.Timestamp}/>
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
                </Section>
            )}

            {hasPrometheus && (
                <ErrorBoundary inline>
                    <MetricsPanel
                        header="Metrics"
                        charts={[
                            {
                                title: "CPU Usage",
                                query: `sum(rate(container_cpu_usage_seconds_total{container_label_com_docker_swarm_service_name="${name}"}[5m])) * 100`,
                                unit: "%",
                                thresholds: cpuThresholds(service),
                                yMin: 0,
                            },
                            {
                                title: "Memory Usage",
                                query: `sum(container_memory_usage_bytes{container_label_com_docker_swarm_service_name="${name}"})`,
                                unit: "bytes",
                                thresholds: memoryThresholds(service),
                                yMin: 0,
                                color: "#34d399",
                            },
                        ]}
                    />
                </ErrorBoundary>
            )}

            {/* Container configuration */}
            {hasContainerConfig && (
                <Section title="Container Configuration" defaultOpen={false}>
                    <KVTable
                        rows={[
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
            )}

            {/* Environment variables */}
            {cs.Env && cs.Env.length > 0 && (
                <Section title="Environment Variables" defaultOpen={false}>
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
                                    <tr key={env} className="border-b last:border-b-0">
                                        <td className="p-3 font-mono text-xs">{key}</td>
                                        <td className="p-3 font-mono text-xs break-all">{val}</td>
                                    </tr>
                                );
                            })}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {/* Healthcheck */}
            {cs.Healthcheck && (
                <Section title="Healthcheck" defaultOpen={false}>
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
                </Section>
            )}

            {/* Labels */}
            {nonStackLabels.length > 0 && (
                <Section title="Labels" defaultOpen={false}>
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
                </Section>
            )}

            {/* Ports */}
            {service.Endpoint?.Ports && service.Endpoint.Ports.length > 0 && (
                <Section title="Ports" defaultOpen={false}>
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
                                <tr key={i} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">{port.PublishedPort}</td>
                                    <td className="p-3 text-sm">{port.TargetPort}</td>
                                    <td className="p-3 text-sm">{port.Protocol}</td>
                                    <td className="p-3 text-sm">{port.PublishMode}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {/* Mounts */}
            {cs.Mounts && cs.Mounts.length > 0 && (
                <Section title="Mounts" defaultOpen={false}>
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
                                <tr key={i} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">{m.Type}</td>
                                    <td className="p-3 font-mono text-xs">{m.Source || "\u2014"}</td>
                                    <td className="p-3 font-mono text-xs">{m.Target}</td>
                                    <td className="p-3 text-sm">{m.ReadOnly ? "yes" : "no"}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {/* Configs */}
            {cs.Configs && cs.Configs.length > 0 && (
                <Section title="Configs" defaultOpen={false}>
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
                                <tr key={cfg.ConfigID} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">{cfg.ConfigName}</td>
                                    <td className="p-3 font-mono text-xs">{cfg.File?.Name || "\u2014"}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {/* Secrets */}
            {cs.Secrets && cs.Secrets.length > 0 && (
                <Section title="Secrets" defaultOpen={false}>
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
                                <tr key={s.SecretID} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">{s.SecretName}</td>
                                    <td className="p-3 font-mono text-xs">{s.File?.Name || "\u2014"}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {/* Deploy: Resources, Placement, Restart, Update, Rollback */}
            <Section title="Deploy Configuration" defaultOpen={false}>
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
                                    ...(
                                        tt.LogDriver.Options
                                            ? Object.entries(tt.LogDriver.Options).map(
                                                ([k, v]) => [k, v] as [string, string],
                                            )
                                            : []
                                    ),
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
                            <KVTable rows={updateConfigRows(service.Spec.UpdateConfig)}/>
                        </div>
                    )}

                    {/* Rollback config */}
                    {service.Spec.RollbackConfig && (
                        <div>
                            <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-2">
                                Rollback Config
                            </h3>
                            <KVTable rows={updateConfigRows(service.Spec.RollbackConfig)}/>
                        </div>
                    )}
                </div>
            </Section>

            {history.length > 0 && (
                <Section title="Recent Activity">
                    <ActivityFeed entries={history}/>
                </Section>
            )}

            <ErrorBoundary inline>
                <LogViewer serviceId={id!} header="Logs"/>
            </ErrorBoundary>
        </div>
    );
}

function sectionKey(title: string) {
    return `section:${title.toLowerCase().replace(/\s+/g, "-")}`;
}

function Section({
    title,
    children,
    controls,
    defaultOpen = true,
}: {
    title: string;
    children: React.ReactNode;
    controls?: React.ReactNode;
    defaultOpen?: boolean;
}) {
    const [open, setOpen] = useState(() => {
        const stored = localStorage.getItem(sectionKey(title));
        return stored !== null ? stored === "1" : defaultOpen;
    });
    const toggle = useCallback(() => {
        setOpen((prev) => {
            localStorage.setItem(sectionKey(title), prev ? "0" : "1");
            return !prev;
        });
    }, [title]);

    return (
        <div>
            <div className="flex items-center gap-2 mb-3 min-h-8">
                <button
                    type="button"
                    onClick={toggle}
                    className="flex items-center gap-1.5 text-sm font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                >
                    <ChevronRight
                        className={`h-4 w-4 transition-transform ${open ? "rotate-90" : ""}`}
                    />
                    {title}
                </button>
                {open && controls && (
                    <div className="flex items-center gap-2 ml-auto">
                        {controls}
                    </div>
                )}
            </div>
            {open && children}
        </div>
    );
}

function KVTable({rows}: { rows: (false | undefined | null | 0 | "" | [string, React.ReactNode])[] }) {
    const valid = rows.filter((row): row is [string, React.ReactNode] => !!row && !!row[1]);
    if (valid.length === 0) {
        return null;
    }
    return (
        <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
                <tbody>
                {valid.map(([k, v]) => (
                    <tr key={k} className="border-b last:border-b-0">
                        <td className="p-3 text-sm font-medium text-muted-foreground w-1/3">{k}</td>
                        <td className="p-3 font-mono text-xs break-all">{v}</td>
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
        cfg.Delay !=
        null &&
        (
            ["Delay", formatNs(cfg.Delay)] as [string, string]
        ),
        cfg.FailureAction &&
        (
            ["Failure Action", cfg.FailureAction] as [string, string]
        ),
        cfg.Monitor !=
        null &&
        (
            ["Monitor", formatNs(cfg.Monitor)] as [string, string]
        ),
        cfg.MaxFailureRatio != null &&
        (
            ["Max Failure Ratio", String(cfg.MaxFailureRatio)] as [string, string]
        ),
        cfg.Order &&
        (
            ["Order", cfg.Order] as [string, string]
        ),
    ];
}

function formatNs(ns: number): string {
    if (ns >= 60_000_000_000) {
        return `${Math.round(ns / 60_000_000_000)}m`;
    }
    if (ns >= 1_000_000_000) {
        return `${Math.round(ns / 1_000_000_000)}s`;
    }
    if (ns >= 1_000_000) {
        return `${Math.round(ns / 1_000_000)}ms`;
    }
    return `${ns}ns`;
}

function formatCpu(nanoCpus: number): string {
    return `${(
        nanoCpus / 1_000_000_000
    ).toFixed(2)} cores`;
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
            value: (
                res.Reservations.NanoCPUs / 1e9
            ) * 100,
            color: "#3b82f6",
            dash: [12, 6],
        });
    }
    if (res.Limits?.NanoCPUs) {
        out.push({
            label: "Limit",
            value: (
                res.Limits.NanoCPUs / 1e9
            ) * 100,
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
