import { statusTone } from "./statusColor";
import { isTerminalTaskState } from "./taskState";
import type { Task } from "@/api/types";

/** Epoch milliseconds, inclusive at both ends. */
export interface TimelineWindow {
  start: number;
  end: number;
}

export interface TimelineBar {
  task: Task;
  start: number;
  end: number;
  ongoing: boolean;
  failed: boolean;
  error: string | undefined;
}

export interface TimelineRow {
  key: string;
  label: string;
  bars: TimelineBar[];
}

export interface Timeline {
  rows: TimelineRow[];

  /** When the oldest task Swarm still retains was created. */
  earliest: number | undefined;
}

interface Group {
  key: string;
  label: string;
  order: [string, number];
}

/**
 * On a service page a row is a slot, or a node for a global service, whose
 * tasks have no slot. On a node page it is a service's slot, so a replica
 * restarting in place reads as one row of short bars.
 */
function groupOf(task: Task, variant: "service" | "node"): Group {
  const slot = task.Slot ?? 0;

  if (variant === "node") {
    const name = task.ServiceName || task.ServiceID.slice(0, 12);

    return slot > 0
      ? { key: `${task.ServiceID}:${slot}`, label: `${name} #${slot}`, order: [name, slot] }
      : { key: task.ServiceID, label: name, order: [name, 0] };
  }

  if (slot > 0) {
    return { key: `slot:${slot}`, label: `Replica #${slot}`, order: ["", slot] };
  }

  const nodeID = task.NodeID ?? "";
  const hostname = task.NodeHostname || nodeID.slice(0, 12);

  return { key: `node:${nodeID}`, label: hostname, order: [hostname, 0] };
}

/** Docker's zero time parses to a year-one date, which no task was created at. */
function startOf(task: Task): number {
  const created = Date.parse(task.CreatedAt);

  return Number.isFinite(created) && created > 0 ? created : Date.parse(task.Status.Timestamp);
}

function barOf(task: Task, now: number): TimelineBar {
  const { State, Err, ContainerStatus, Timestamp } = task.Status;
  const start = startOf(task);
  const stopped = isTerminalTaskState(State);
  const exitCode = stopped ? ContainerStatus?.ExitCode : undefined;

  return {
    task,
    start,
    end: stopped ? Math.max(start, Date.parse(Timestamp)) : now,
    ongoing: !stopped,
    failed: statusTone(State) === "danger",
    error: Err || (exitCode ? `exit ${exitCode}` : undefined),
  };
}

export function buildTimeline(
  tasks: Task[],
  variant: "service" | "node",
  timeWindow: TimelineWindow,
  now: number,
): Timeline {
  const groups = new Map<string, Group & { bars: TimelineBar[] }>();
  let earliest: number | undefined;

  for (const task of tasks) {
    const bar = barOf(task, now);

    earliest = earliest === undefined ? bar.start : Math.min(earliest, bar.start);

    if (bar.end < timeWindow.start || bar.start > timeWindow.end) {
      continue;
    }

    const group = groupOf(task, variant);
    const existing = groups.get(group.key);

    if (existing) {
      existing.bars.push(bar);
    } else {
      groups.set(group.key, { ...group, bars: [bar] });
    }
  }

  const rows = [...groups.values()]
    .sort(({ order: [leftName, leftSlot] }, { order: [rightName, rightSlot] }) =>
      leftName === rightName ? leftSlot - rightSlot : leftName.localeCompare(rightName),
    )
    .map(({ key, label, bars }) => ({
      key,
      label,
      bars: bars.sort((left, right) => left.start - right.start),
    }));

  return { rows, earliest };
}
