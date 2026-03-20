import type { Task } from "../api/types";
import type { TaskMetricsData } from "../hooks/useTaskMetrics";
import { statusColor } from "../lib/statusColor";
import CollapsibleSection from "./CollapsibleSection";
import { TaskSparkline } from "./metrics";
import ResourceName from "./ResourceName";
import TaskStateFilter, { isActiveTask } from "./TaskStateFilter";
import TaskStatusBadge from "./TaskStatusBadge";
import TimeAgo from "./TimeAgo";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";

type Variant = "node" | "service";

interface TasksTableProps {
  tasks: Task[];
  variant: Variant;
  metrics?: Map<string, TaskMetricsData>;
}

export default function TasksTable({ tasks, variant, metrics }: TasksTableProps) {
  const [stateFilter, setStateFilter] = useState<string | null>(null);
  const filteredTasks = useMemo(() => {
    const filtered =
      stateFilter === "__all__"
        ? tasks
        : stateFilter
          ? tasks.filter(({ Status: { State } }) => State === stateFilter)
          : tasks.filter(isActiveTask);

    const stateOrder: Record<string, number> = {
      new: 0,
      pending: 1,
      assigned: 2,
      accepted: 3,
      ready: 4,
      preparing: 5,
      starting: 6,
      running: 7,
      complete: 8,
      failed: 9,
      shutdown: 10,
      rejected: 11,
      orphaned: 12,
      remove: 13,
    };
    const stateWeight = (state: string) => stateOrder[state] ?? 99;

    return [...filtered].sort((a, b) => {
      const weight = stateWeight(a.Status.State) - stateWeight(b.Status.State);

      if (weight !== 0) {
        return weight;
      }

      return new Date(b.Status.Timestamp).getTime() - new Date(a.Status.Timestamp).getTime();
    });
  }, [tasks, stateFilter]);

  if (tasks.length === 0) {
    return null;
  }

  return (
    <CollapsibleSection
      title="Tasks"
      controls={
        <TaskStateFilter
          tasks={tasks}
          active={stateFilter}
          onChange={setStateFilter}
        />
      }
    >
      <div className="max-h-96 overflow-auto rounded-lg border">
        <table className="w-full min-w-max whitespace-nowrap">
          <thead className="sticky top-0 z-10 bg-background">
            <tr className="border-b bg-muted/50">
              {variant === "node" && <th className="p-3 text-left text-sm font-medium">Service</th>}
              <th className="p-3 text-left text-sm font-medium">Task</th>
              <th className="p-3 text-left text-sm font-medium">State</th>
              {metrics && <th className="p-3 text-left text-sm font-medium">CPU</th>}
              {metrics && <th className="p-3 text-left text-sm font-medium">Memory</th>}
              {variant === "service" && <th className="p-3 text-left text-sm font-medium">Node</th>}
              <th className="p-3 text-left text-sm font-medium">Image</th>
              <th className="p-3 text-left text-sm font-medium">Desired</th>
              <th className="p-3 text-left text-sm font-medium">Error</th>
              <th className="p-3 text-left text-sm font-medium">Timestamp</th>
            </tr>
          </thead>
          <tbody>
            {filteredTasks.map(
              ({
                DesiredState,
                ID,
                NodeHostname,
                NodeID,
                ServiceID,
                ServiceName,
                Slot,
                Spec: {
                  ContainerSpec: { Image },
                },
                Status: { ContainerStatus, Err, State, Timestamp },
              }) => {
                const exitCode = ContainerStatus?.ExitCode;
                const errorMessage = Err || (exitCode && exitCode !== 0 ? `exit ${exitCode}` : "");

                return (
                  <tr
                    key={ID}
                    className="border-b last:border-b-0"
                  >
                    {variant === "node" && (
                      <td className="p-3 text-sm whitespace-nowrap">
                        <Link
                          to={`/services/${ServiceID}`}
                          className="text-link hover:underline"
                        >
                          <ResourceName name={ServiceName || ServiceID.slice(0, 12)} />
                        </Link>
                      </td>
                    )}

                    <td className="p-3 text-sm">
                      <span className="inline-flex items-center gap-2">
                        <span className={`size-2 shrink-0 rounded-full ${statusColor(State)}`} />
                        <Link
                          to={`/tasks/${ID}`}
                          className="text-link hover:underline"
                        >
                          {variant === "node" && Slot ? (
                            `Replica #${Slot}`
                          ) : (
                            <span className="font-mono">{ID.slice(0, 12)}</span>
                          )}
                        </Link>
                      </span>
                    </td>

                    <td className="p-3 text-sm">
                      <TaskStatusBadge state={State} />
                    </td>

                    {metrics && (
                      <td className="p-3 text-sm">
                        {State === "running" ? (
                          <TaskSparkline
                            data={metrics.get(ID)?.cpu}
                            currentValue={metrics.get(ID)?.currentCpu}
                            type="cpu"
                          />
                        ) : (
                          "\u2014"
                        )}
                      </td>
                    )}

                    {metrics && (
                      <td className="p-3 text-sm">
                        {State === "running" ? (
                          <TaskSparkline
                            data={metrics.get(ID)?.memory}
                            currentValue={metrics.get(ID)?.currentMemory}
                            type="memory"
                          />
                        ) : (
                          "\u2014"
                        )}
                      </td>
                    )}

                    {variant === "service" && (
                      <td className="p-3 text-sm">
                        <Link
                          to={`/nodes/${NodeID}`}
                          className="text-link hover:underline"
                        >
                          {NodeHostname || NodeID.slice(0, 12)}
                        </Link>
                      </td>
                    )}

                    <td className="p-3 text-sm">
                      <span className="font-mono text-xs">{Image.split("@")[0]}</span>
                    </td>
                    <td className="p-3 text-sm">{DesiredState}</td>
                    <td className="p-3 text-sm text-red-600 dark:text-red-400">{errorMessage}</td>
                    <td className="p-3 text-sm text-muted-foreground">
                      {Timestamp ? <TimeAgo date={Timestamp} /> : "\u2014"}
                    </td>
                  </tr>
                );
              },
            )}
          </tbody>
        </table>
      </div>
    </CollapsibleSection>
  );
}
