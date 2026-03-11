import type React from "react";
import {useCallback, useEffect, useState} from "react";
import {Link, useParams} from "react-router-dom";
import {api} from "../api/client";
import type {StackDetail as StackDetailType, Task} from "../api/types";
import FetchError from "../components/FetchError";
import {LoadingDetail} from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import {useSSE} from "../hooks/useSSE";

function Section({title, children}: { title: string; children: React.ReactNode }) {
    return (
        <div>
            <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
                {title}
            </h2>

            {children}
        </div>
    );
}

export default function StackDetail() {
    const {name} = useParams<{ name: string }>();
    const [stack, setStack] = useState<StackDetailType | null>(null);
    const [error, setError] = useState(false);
    const [taskCounts, setTaskCounts] = useState<Record<string, { running: number; total: number }>>(
        {},
    );

    const fetchData = useCallback(() => {
        if (name) {
            api
                .stack(name)
                .then(setStack)
                .catch(() => setError(true));
        }
    }, [name]);

    useEffect(fetchData, [fetchData]);

    useSSE(["stack", "service", "task"], useCallback(() => {
        fetchData();
    }, [fetchData]));

    useEffect(() => {
        if (!stack?.services?.length) {
            return;
        }

        let cancelled = false;

        Promise.all(
            stack.services.map(({ID}) =>
                api
                    .serviceTasks(ID)
                    .then((tasks: Task[]) => [ID, tasks] as const)
                    .catch(() => [ID, []] as const),
            ),
        ).then((results) => {
            if (cancelled) {
                return;
            }

            const counts: Record<string, { running: number; total: number }> = {};

            for (const [id, tasks] of results) {
                counts[id] = {
                    running: tasks.filter(({Status: {State}}) => State === "running").length,
                    total: tasks.length,
                };
            }
            setTaskCounts(counts);
        });

        return () => {
            cancelled = true;
        };
    }, [stack]);

    if (error) {
        return <FetchError message="Failed to load stack"/>;
    }
    if (!stack) {
        return <LoadingDetail/>;
    }

    const parts: string[] = [];
    if (stack.services?.length) {
        parts.push(`${stack.services.length} service${stack.services.length !== 1 ? "s" : ""}`);
    }
    if (stack.configs?.length) {
        parts.push(`${stack.configs.length} config${stack.configs.length !== 1 ? "s" : ""}`);
    }
    if (stack.secrets?.length) {
        parts.push(`${stack.secrets.length} secret${stack.secrets.length !== 1 ? "s" : ""}`);
    }
    if (stack.networks?.length) {
        parts.push(`${stack.networks.length} network${stack.networks.length !== 1 ? "s" : ""}`);
    }
    if (stack.volumes?.length) {
        parts.push(`${stack.volumes.length} volume${stack.volumes.length !== 1 ? "s" : ""}`);
    }
    const subtitle = parts.join(", ");

    return (
        <div className="flex flex-col gap-6">
            <PageHeader
                title={stack.name}
                subtitle={subtitle}
                breadcrumbs={[{label: "Stacks", to: "/stacks"}, {label: stack.name}]}
            />

            {stack.services?.length > 0 && (
                <Section title="Services">
                    <div className="overflow-x-auto rounded-lg border">
                        <table className="w-full">
                            <thead className="sticky top-0 z-10 bg-background">
                            <tr className="border-b bg-muted/50">
                                <th className="text-left p-3 text-sm font-medium">Name</th>
                                <th className="text-left p-3 text-sm font-medium">Image</th>
                                <th className="text-left p-3 text-sm font-medium">Mode</th>
                                <th className="text-left p-3 text-sm font-medium">Tasks</th>
                            </tr>
                            </thead>
                            <tbody>
                            {stack.services.map(({ID, Spec: {Mode, Name, TaskTemplate}}) => (
                                <tr key={ID} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">
                                        <Link
                                            to={`/services/${ID}`}
                                            className="text-link hover:underline font-medium"
                                        >
                                            <ResourceName name={Name || ID}/>
                                        </Link>
                                    </td>
                                    <td className="p-3 font-mono text-xs">
                                        {TaskTemplate.ContainerSpec.Image.split("@")[0]}
                                    </td>
                                    <td className="p-3 text-sm">
                                        {Mode.Replicated ? "replicated" : "global"}
                                    </td>
                                    <td className="p-3 text-sm tabular-nums">
                                        {taskCounts[ID] ? (
                                            <span>
                                                <span
                                                    data-healthy={taskCounts[ID].running === taskCounts[ID].total || undefined}
                                                    className="text-yellow-600 data-healthy:text-green-600"
                                                >
                                                    {taskCounts[ID].running}
                                                </span>
                                                /{taskCounts[ID].total}
                                            </span>
                                        ) : (
                                            "—"
                                        )}
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {stack.configs?.length > 0 && (
                <Section title="Configs">
                    <div className="overflow-x-auto rounded-lg border">
                        <table className="w-full">
                            <tbody>
                            {stack.configs.map(({ID, Spec: {Name}}) => (
                                <tr key={ID} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">
                                        <Link
                                            to={`/configs/${ID}`}
                                            className="text-link hover:underline font-medium"
                                        >
                                            <ResourceName name={Name || ID}/>
                                        </Link>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {stack.secrets?.length > 0 && (
                <Section title="Secrets">
                    <div className="overflow-x-auto rounded-lg border">
                        <table className="w-full">
                            <tbody>
                            {stack.secrets.map(({ID, Spec: {Name}}) => (
                                <tr key={ID} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">
                                        <Link
                                            to={`/secrets/${ID}`}
                                            className="text-link hover:underline font-medium"
                                        >
                                            <ResourceName name={Name || ID}/>
                                        </Link>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {stack.networks?.length > 0 && (
                <Section title="Networks">
                    <div className="overflow-x-auto rounded-lg border">
                        <table className="w-full">
                            <thead className="sticky top-0 z-10 bg-background">
                            <tr className="border-b bg-muted/50">
                                <th className="text-left p-3 text-sm font-medium">Name</th>
                                <th className="text-left p-3 text-sm font-medium">Driver</th>
                            </tr>
                            </thead>
                            <tbody>
                            {stack.networks.map(({Driver, Id, Name}) => (
                                <tr key={Id} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">
                                        <Link
                                            to={`/networks/${Id}`}
                                            className="text-link hover:underline font-medium"
                                        >
                                            <ResourceName name={Name}/>
                                        </Link>
                                    </td>
                                    <td className="p-3 text-sm">{Driver}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}

            {stack.volumes?.length > 0 && (
                <Section title="Volumes">
                    <div className="overflow-x-auto rounded-lg border">
                        <table className="w-full">
                            <tbody>
                            {stack.volumes.map(({Name}) => (
                                <tr key={Name} className="border-b last:border-b-0">
                                    <td className="p-3 text-sm">
                                        <Link
                                            to={`/volumes/${Name}`}
                                            className="text-link hover:underline font-medium"
                                        >
                                            <ResourceName name={Name}/>
                                        </Link>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </Section>
            )}
        </div>
    );
}
