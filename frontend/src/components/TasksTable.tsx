import {useMemo, useState} from "react";
import {Link} from "react-router-dom";
import type {Task} from "../api/types";
import {statusColor} from "../lib/statusColor";
import CollapsibleSection from "./CollapsibleSection";
import ResourceName from "./ResourceName";
import TaskStateFilter from "./TaskStateFilter";
import TaskStatusBadge from "./TaskStatusBadge";
import TimeAgo from "./TimeAgo";

type Variant = "node" | "service";

interface TasksTableProps {
    tasks: Task[];
    variant: Variant;
}

export default function TasksTable({tasks, variant}: TasksTableProps) {
    const [stateFilter, setStateFilter] = useState<string | null>(null);

    const filteredTasks = useMemo(() => {
        const filtered = stateFilter
            ? tasks.filter(({Status: {State}}) => State === stateFilter)
            : tasks;

        return [...filtered].sort(
            (a, b) => new Date(b.Status.Timestamp).getTime() - new Date(a.Status.Timestamp).getTime(),
        );
    }, [tasks, stateFilter]);

    if (tasks.length === 0) {
        return null;
    }

    return (
        <CollapsibleSection
            title="Tasks"
            controls={<TaskStateFilter tasks={tasks} active={stateFilter} onChange={setStateFilter}/>}
        >
            <div className="overflow-auto rounded-lg border max-h-96">
                <table className="w-full">
                    <thead className="sticky top-0 z-10 bg-background">
                    <tr className="border-b bg-muted/50">
                        {variant === "node" && <th className="text-left p-3 text-sm font-medium">Service</th>}
                        <th className="text-left p-3 text-sm font-medium">Task</th>
                        <th className="text-left p-3 text-sm font-medium">State</th>
                        {variant === "service" && <th className="text-left p-3 text-sm font-medium">Node</th>}
                        <th className="text-left p-3 text-sm font-medium">Desired</th>
                        <th className="text-left p-3 text-sm font-medium">Error</th>
                        <th className="text-left p-3 text-sm font-medium">Timestamp</th>
                    </tr>
                    </thead>
                    <tbody>
                    {filteredTasks.map((task) => {
                        const {
                            DesiredState,
                            ID,
                            NodeHostname,
                            NodeID,
                            ServiceID,
                            ServiceName,
                            Slot,
                            Status: {ContainerStatus, Err, State, Timestamp},
                        } = task;
                        const exitCode = ContainerStatus?.ExitCode;
                        const errorMessage = Err ||
                            (
                                exitCode && exitCode !== 0 ? `exit ${exitCode}` : ""
                            );

                        return (
                            <tr key={ID} className="border-b last:border-b-0">
                                {variant === "node" && (
                                    <td className="p-3 text-sm whitespace-nowrap">
                                        <Link to={`/services/${ServiceID}`} className="text-link hover:underline">
                                            <ResourceName name={ServiceName || ServiceID.slice(0, 12)}/>
                                        </Link>
                                    </td>
                                )}

                                <td className="p-3 text-sm">
                                    <span className="inline-flex items-center gap-2">
                                        <span className={`shrink-0 size-2 rounded-full ${statusColor(State)}`}/>
                                        <Link to={`/tasks/${ID}`} className="text-link hover:underline">
                                            {variant === "node" && Slot ? `Replica #${Slot}` : ID.slice(0, 12)}
                                        </Link>
                                    </span>
                                </td>

                                <td className="p-3 text-sm">
                                    <TaskStatusBadge state={State}/>
                                </td>

                                {variant === "service" && (
                                    <td className="p-3 text-sm">
                                        <Link to={`/nodes/${NodeID}`} className="text-link hover:underline">
                                            {NodeHostname || NodeID.slice(0, 12)}
                                        </Link>
                                    </td>
                                )}

                                <td className="p-3 text-sm">{DesiredState}</td>
                                <td className="p-3 text-sm text-red-600 dark:text-red-400">{errorMessage}</td>
                                <td className="p-3 text-sm text-muted-foreground">
                                    {Timestamp ? <TimeAgo date={Timestamp}/> : "\u2014"}
                                </td>
                            </tr>
                        );
                    })}
                    </tbody>
                </table>
            </div>
        </CollapsibleSection>
    );
}
