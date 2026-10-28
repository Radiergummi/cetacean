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
}

/** A replicated service's rows are slots, a global one's nodes. */
export type TimelineVariant = "service" | "global" | "node";

interface Group {
  key: string;
  label: string;
  order: [string, number];
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
      ? { key: `${task.ServiceID}:${slot}`, label: `${name} #${slot}`, order: [name, slot] }
      : { key: task.ServiceID, label: name, order: [name, 0] };
  }

  if (variant === "service") {
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

function barOf(task: Task, now: number): Omit<TimelineBar, "lane"> {
  const { State, Err, ContainerStatus, Timestamp } = task.Status;
  const start = startOf(task);
  const stopped = isTerminalTaskState(State);
  const exitCode = stopped ? ContainerStatus?.ExitCode : undefined;
  const failed = statusTone(State) === "danger";

  return {
    task,
    start,
    end: Math.max(start, stopped ? Date.parse(Timestamp) : now),
    ongoing: !stopped,
    failed,
    // A task stopped by an update exits non-zero too; that is no error.
    error: Err || (failed && exitCode ? `exit ${exitCode}` : undefined),
  };
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
  const groups = new Map<string, Group & { bars: Omit<TimelineBar, "lane">[]; earliest: number }>();

  for (const task of tasks) {
    const bar = barOf(task, now);
    const group = groupOf(task, variant);
    let existing = groups.get(group.key);

    if (!existing) {
      existing = { ...group, bars: [], earliest: bar.start };
      groups.set(group.key, existing);
    }

    existing.earliest = Math.min(existing.earliest, bar.start);

    if (bar.end >= timeWindow.start && bar.start <= timeWindow.end) {
      existing.bars.push(bar);
    }
  }

  return [...groups.values()]
    .filter(({ bars }) => bars.length > 0)
    .sort(({ order: [leftName, leftSlot] }, { order: [rightName, rightSlot] }) =>
      leftName === rightName ? leftSlot - rightSlot : leftName.localeCompare(rightName),
    )
    .map(({ key, label, bars, earliest }) => ({ key, label, earliest, ...laneBars(bars) }));
}
