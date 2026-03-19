import type { Task } from "../api/types";
import type { Segment } from "./SegmentedControl";
import SegmentedControl from "./SegmentedControl";
import { useEffect, useMemo } from "react";

const all = "__all__" as const;

const knownStates = [
  "running",
  "failed",
  "complete",
  "shutdown",
  "ready",
  "starting",
  "assigned",
  "accepted",
  "rejected",
  "remove",
  "orphaned",
  "pending",
] as const;

export default function TaskStateFilter({
  tasks,
  active,
  onChange,
}: {
  tasks: Task[];
  active: string | null;
  onChange: (state: string | null) => void;
}) {
  const segments = useMemo(() => {
    const counts = new Map<string, number>();

    for (const {
      Status: { State },
    } of tasks) {
      counts.set(State, (counts.get(State) || 0) + 1);
    }

    const segments: Segment<string>[] = [{ value: all, label: "All", badge: tasks.length }];
    const enabled: Segment<string>[] = [];
    const disabled: Segment<string>[] = [];

    for (const state of knownStates) {
      const count = counts.get(state) ?? 0;
      const label = state.charAt(0).toUpperCase() + state.slice(1);

      if (count > 0) {
        enabled.push({ value: state, label, badge: count });
      } else {
        disabled.push({ value: state, label, disabled: true });
      }
    }

    return [...segments, ...enabled, ...disabled];
  }, [tasks]);

  // Reset filter if the selected state has no tasks
  useEffect(() => {
    if (active && segments.find(({ value }) => value === active)?.disabled) {
      onChange(null);
    }
  }, [active, segments, onChange]);

  return (
    <SegmentedControl
      segments={segments}
      value={active ?? all}
      onChange={(value) => onChange(value === all ? null : value)}
      max={3}
    />
  );
}
