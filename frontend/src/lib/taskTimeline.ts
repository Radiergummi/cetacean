import { statusTone } from "./statusColor";
import { isTerminalTaskState } from "./taskState";
import type { RestartPolicy, Task } from "@/api/types";

/** Epoch milliseconds, inclusive at both ends. */
export interface TimelineWindow {
  start: number;
  end: number;
}

export interface TimelineBar {
  task: Task;
  start: number;
  end: number;
  /** Tasks of one row that overlap in time take separate lanes. */
  lane: number;
  ongoing: boolean;
  failed: boolean;
  error: string | undefined;
}

export interface TimelineRow {
  key: string;
  label: string;
  bars: TimelineBar[];
  lanes: number;

  /** When the row's earliest known task was created; only tasks of this row count. */
  earliest: number;

  /** How many tasks the row holds, inside the window or not. */
  count: number;

  unit: TimelineUnit;

  /** The newest task's, which is its service's once an update has rolled out. */
  restartPolicy: RestartPolicy | undefined;
}

/**
 * What Swarm keeps a task history for: a replicated service's slot, or a
 * global service's node.
 */
export type TimelineUnit = "replica" | "node";

/** A replicated service's rows are slots, a global one's nodes. */
export type TimelineVariant = "service" | "global" | "node";

interface Group {
  key: string;
  label: string;
  order: [string, number];
  unit: TimelineUnit;
}

/**
 * On a service page a row is a slot, or a node for a global service. On a
 * node page it is a service's slot, so a replica restarting in place reads as
 * one row of short bars.
 */
function groupOf(task: Task, variant: TimelineVariant): Group {
  const slot = task.Slot ?? 0;

  if (variant === "node") {
    const name = task.ServiceName || task.ServiceID.slice(0, 12);

    return slot > 0
      ? {
          key: `${task.ServiceID}:${slot}`,
          label: `${name} #${slot}`,
          order: [name, slot],
          unit: "replica",
        }
      : { key: task.ServiceID, label: name, order: [name, 0], unit: "node" };
  }

  if (variant === "service") {
    return { key: `slot:${slot}`, label: `Replica #${slot}`, order: ["", slot], unit: "replica" };
  }

  const nodeID = task.NodeID ?? "";
  const hostname = task.NodeHostname || nodeID.slice(0, 12);

  return { key: `node:${nodeID}`, label: hostname, order: [hostname, 0], unit: "node" };
}

/** Docker's zero time parses to a year-one date, which no task was created at. */
function startOf(task: Task): number {
  const created = Date.parse(task.CreatedAt);

  return Number.isFinite(created) && created > 0 ? created : Date.parse(task.Status.Timestamp);
}

/** From creation to the last state change, or to now while it has not stopped. */
function spanOf(task: Task, now: number): { start: number; end: number; stopped: boolean } {
  const start = startOf(task);
  const stopped = isTerminalTaskState(task.Status.State);

  return {
    start,
    end: Math.max(start, stopped ? Date.parse(task.Status.Timestamp) : now),
    stopped,
  };
}

function barOf(task: Task, now: number): Omit<TimelineBar, "lane"> {
  const { State, Err, ContainerStatus } = task.Status;
  const { start, end, stopped } = spanOf(task, now);
  const exitCode = stopped ? ContainerStatus?.ExitCode : undefined;
  const failed = statusTone(State) === "danger";

  return {
    task,
    start,
    end,
    ongoing: !stopped,
    failed,
    // A task stopped by an update exits non-zero too; that is no error.
    error: Err || (failed && exitCode ? `exit ${exitCode}` : undefined),
  };
}

/**
 * How many tasks Swarm keeps per replica, or per node for a global service; a
 * negative limit never prunes. A restart policy's attempts override the swarm's.
 */
export function retentionLimitOf(
  clusterLimit: number,
  restartPolicy: RestartPolicy | undefined,
): number {
  const maxAttempts = restartPolicy?.MaxAttempts ?? 0;

  return maxAttempts > 0 ? maxAttempts + 1 : clusterLimit;
}

function overlaps({ start, end }: { start: number; end: number }, timeWindow: TimelineWindow) {
  return end >= timeWindow.start && start <= timeWindow.end;
}

/**
 * Whether a task was alive at some point inside the window: the rule the
 * timeline draws by, so the list beside it shows the same tasks.
 */
export function isInWindow(task: Task, timeWindow: TimelineWindow, now: number): boolean {
  const span = spanOf(task, now);

  // A task whose times do not parse cannot be placed, so it is not hidden.
  return !Number.isFinite(span.end) || overlaps(span, timeWindow);
}

/** Puts each bar in the first lane free at its start. */
function laneBars(bars: Omit<TimelineBar, "lane">[]): { bars: TimelineBar[]; lanes: number } {
  const laneEnds: number[] = [];
  const placed = bars
    .sort((left, right) => left.start - right.start)
    .map((bar) => {
      let lane = laneEnds.findIndex((end) => end <= bar.start);

      if (lane === -1) {
        lane = laneEnds.length;
      }

      laneEnds[lane] = bar.end;

      return { ...bar, lane };
    });

  return { bars: placed, lanes: Math.max(1, laneEnds.length) };
}

export function buildTimeline(
  tasks: Task[],
  variant: TimelineVariant,
  timeWindow: TimelineWindow,
  now: number,
): TimelineRow[] {
  const groups = new Map<
    string,
    Group & {
      bars: Omit<TimelineBar, "lane">[];
      earliest: number;
      latest: number;
      count: number;
      restartPolicy: RestartPolicy | undefined;
    }
  >();

  for (const task of tasks) {
    const bar = barOf(task, now);
    const group = groupOf(task, variant);
    let existing = groups.get(group.key);

    if (!existing) {
      existing = {
        ...group,
        bars: [],
        earliest: bar.start,
        latest: bar.start,
        count: 0,
        restartPolicy: task.Spec.RestartPolicy,
      };
      groups.set(group.key, existing);
    }

    if (bar.start > existing.latest) {
      existing.latest = bar.start;
      existing.restartPolicy = task.Spec.RestartPolicy;
    }

    existing.earliest = Math.min(existing.earliest, bar.start);
    existing.count++;

    if (overlaps(bar, timeWindow)) {
      existing.bars.push(bar);
    }
  }

  return [...groups.values()]
    .filter(({ bars }) => bars.length > 0)
    .sort(({ order: [leftName, leftSlot] }, { order: [rightName, rightSlot] }) =>
      leftName === rightName ? leftSlot - rightSlot : leftName.localeCompare(rightName),
    )
    .map(({ key, label, bars, earliest, count, restartPolicy, unit }) => ({
      key,
      label,
      earliest,
      count,
      restartPolicy,
      unit,
      ...laneBars(bars),
    }));
}
