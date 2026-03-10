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
import {statusColor} from "../lib/statusColor";
import {ContainerImage, ResourceLink, Timestamp} from "../components/data";
import FetchError from "../components/FetchError";
import ResourceName from "../components/ResourceName";

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

    const taskIds = useMemo(() => new Set(tasks.map((t) => t.ID)), [tasks]);

    useSSE(["service", "task"], (e) => {
        if (e.type === "service" && e.id === id) fetchData();
        if (e.type === "task" && (taskIds.has(e.id) || (e.resource as Record<string, unknown>)?.ServiceID === id)) fetchData();
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
                title={<ResourceName name={name} />}
                breadcrumbs={[{label: "Services", to: "/services"}, {label: <ResourceName name={name} />}]}
            />

            {/* Overview cards */}
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                <ContainerImage image={cs.Image} />
                <ReplicaCard service={service} tasks={tasks} />
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
                <ResourceLink label="Stack" name={labels["com.docker.stack.namespace"]} to={`/stacks/${labels["com.docker.stack.namespace"]}`} />
                <Timestamp label="Created" date={service.CreatedAt} />
                <Timestamp label="Updated" date={service.UpdatedAt} />
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
                                <th className="text-left p-3 text-sm font-medium">Task</th>
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
                                        <td className="p-3 text-sm">
                                            <span className="inline-flex items-center gap-2">
                                                <span className={`shrink-0 w-2 h-2 rounded-full ${statusColor(task.Status.State)}`}/>
                                                <Link to={`/tasks/${task.ID}`} className="text-link hover:underline">
                                                    {task.Slot ? `Replica #${task.Slot}` : task.ID.slice(0, 12)}
                                                </Link>
                                            </span>
                                        </td>
                                        <td className="p-3 text-sm">
                                            <TaskStatusBadge state={task.Status.State}/>
                                        </td>
                                        <td className="p-3 text-sm">
                                            <Link to={`/nodes/${task.NodeID}`} className="text-link hover:underline">
                                                {task.NodeHostname || task.NodeID.slice(0, 12)}
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
                    <div className="flex flex-wrap gap-2">
                        {service.Endpoint.Ports.map((port, i) => (
                            <span
                                key={i}
                                className="inline-flex items-center gap-1.5 rounded-lg border px-3 py-2 text-sm font-mono"
                            >
                                <span className="font-semibold">{port.PublishedPort}</span>
                                <span className="text-muted-foreground">&rarr;</span>
                                <span>{port.TargetPort}/{port.Protocol}</span>
                                <span className="text-xs text-muted-foreground">({port.PublishMode})</span>
                            </span>
                        ))}
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
                                    <td className="p-3 text-sm">
                                        <MountTypeBadge type={m.Type} />
                                    </td>
                                    <td className="p-3 font-mono text-xs">
                                        {m.Type === "volume" && m.Source ? (
                                            <Link to={`/volumes/${m.Source}`} className="text-link hover:underline"><ResourceName name={m.Source} /></Link>
                                        ) : (
                                            m.Source || "\u2014"
                                        )}
                                    </td>
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
                                    <td className="p-3 text-sm">
                                        <Link to={`/configs/${cfg.ConfigID}`} className="text-link hover:underline">
                                            <ResourceName name={cfg.ConfigName} />
                                        </Link>
                                    </td>
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
                                    <td className="p-3 text-sm">
                                        <Link to={`/secrets/${s.SecretID}`} className="text-link hover:underline">
                                            <ResourceName name={s.SecretName} />
                                        </Link>
                                    </td>
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
                            <ResourcesPanel resources={tt.Resources} />
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
                            <PlacementPanel placement={tt.Placement} />
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

function ReplicaCard({service, tasks}: { service: Service; tasks: Task[] }) {
    const replicated = service.Spec.Mode.Replicated;
    if (!replicated) {
        return <InfoCard label="Mode" value="global" />;
    }

    const desired = replicated.Replicas ?? 0;
    const running = tasks.filter((t) => t.Status.State === "running").length;
    const healthy = running >= desired;

    return (
        <div className="rounded-lg border bg-card p-4">
            <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground mb-1">Replicas</div>
            <div className="flex items-center gap-3">
                <span className="text-2xl font-bold tabular-nums">
                    {running}<span className="text-muted-foreground font-normal text-lg">/{desired}</span>
                </span>
                {desired > 0 && (
                    <div className="flex gap-0.5">
                        {Array.from({length: desired}, (_, i) => (
                            <div
                                key={i}
                                className={`h-3 w-3 rounded-sm ${i < running ? "bg-green-500" : "bg-red-400 dark:bg-red-500"}`}
                            />
                        ))}
                    </div>
                )}
            </div>
            {!healthy && (
                <div className="text-xs text-red-600 dark:text-red-400 mt-1">
                    {desired - running} replica{desired - running !== 1 ? "s" : ""} not running
                </div>
            )}
        </div>
    );
}

const mountTypeColors: Record<string, string> = {
    volume: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300",
    bind: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300",
    tmpfs: "bg-purple-100 text-purple-800 dark:bg-purple-900/30 dark:text-purple-300",
};

function MountTypeBadge({type}: { type: string }) {
    const color = mountTypeColors[type] || "bg-muted text-muted-foreground";
    return (
        <span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium ${color}`}>
            {type}
        </span>
    );
}

function parseConstraint(raw: string): { field: string; op: string; value: string; include: boolean } {
    const match = raw.match(/^(.+?)\s*(==|!=)\s*(.+)$/);
    if (!match) return {field: raw, op: "", value: "", include: true};
    return {field: match[1], op: match[2], value: match[3], include: match[2] === "=="};
}

type PlacementShape = NonNullable<Service["Spec"]["TaskTemplate"]["Placement"]>;

function PlacementPanel({placement}: { placement: PlacementShape }) {
    const constraints = placement.Constraints ?? [];
    const preferences = placement.Preferences ?? [];
    const hasContent = constraints.length > 0 || placement.MaxReplicas || preferences.length > 0;

    if (!hasContent) return <p className="text-sm text-muted-foreground">No placement constraints.</p>;

    return (
        <div className="space-y-3">
            {constraints.length > 0 && (
                <div className="overflow-x-auto rounded-lg border">
                    <table className="w-full">
                        <thead>
                        <tr className="border-b bg-muted/50">
                            <th className="text-left p-3 text-sm font-medium w-8"></th>
                            <th className="text-left p-3 text-sm font-medium">Field</th>
                            <th className="text-left p-3 text-sm font-medium">Operator</th>
                            <th className="text-left p-3 text-sm font-medium">Value</th>
                        </tr>
                        </thead>
                        <tbody>
                        {constraints.map((c) => {
                            const parsed = parseConstraint(c);
                            return (
                                <tr key={c} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">
                                        <span className={`inline-block w-2 h-2 rounded-full ${parsed.include ? "bg-green-500" : "bg-red-500"}`} />
                                    </td>
                                    <td className="p-3 font-mono text-xs">{parsed.field}</td>
                                    <td className="p-3 font-mono text-xs text-muted-foreground">{parsed.op}</td>
                                    <td className="p-3 font-mono text-xs">{parsed.value}</td>
                                </tr>
                            );
                        })}
                        </tbody>
                    </table>
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

function ResourceBar({label, reserved, limit, format}: {
    label: string;
    reserved?: number;
    limit?: number;
    format: (v: number) => string;
}) {
    if (!reserved && !limit) return null;
    const max = limit || reserved || 0;
    const reservedPct = reserved && max ? Math.round((reserved / max) * 100) : 0;

    return (
        <div className="space-y-1.5">
            <div className="flex items-center justify-between text-sm">
                <span className="font-medium">{label}</span>
                <div className="flex items-center gap-3 text-xs text-muted-foreground">
                    {reserved != null && <span>Reserved: <span className="font-mono text-foreground">{format(reserved)}</span></span>}
                    {limit != null && <span>Limit: <span className="font-mono text-foreground">{format(limit)}</span></span>}
                </div>
            </div>
            {limit && reserved ? (
                <div className="h-2 rounded-full bg-muted overflow-hidden">
                    <div
                        className="h-full rounded-full bg-blue-500"
                        style={{width: `${reservedPct}%`}}
                    />
                </div>
            ) : null}
        </div>
    );
}

function ResourcesPanel({resources}: { resources: ResourceShape }) {
    const hasCpu = resources.Limits?.NanoCPUs || resources.Reservations?.NanoCPUs;
    const hasMem = resources.Limits?.MemoryBytes || resources.Reservations?.MemoryBytes;
    const hasPids = resources.Limits?.Pids;

    if (!hasCpu && !hasMem && !hasPids) return null;

    return (
        <div className="space-y-4">
            <ResourceBar
                label="CPU"
                reserved={resources.Reservations?.NanoCPUs}
                limit={resources.Limits?.NanoCPUs}
                format={formatCpu}
            />
            <ResourceBar
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
