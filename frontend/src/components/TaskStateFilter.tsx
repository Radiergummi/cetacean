import {useMemo} from "react";
import type {Task} from "../api/types";
import type {Segment} from "./SegmentedControl";
import SegmentedControl from "./SegmentedControl";

const ALL = "__all__";

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
        const mappedStates = new Map<string, number>();

        for (const {Status: {State}} of tasks) {
            mappedStates.set(
                State,
                (
                    mappedStates.get(State) || 0
                ) + 1,
            );
        }

        const segments: Segment<string>[] = [
            {value: ALL, label: "All", badge: tasks.length},
        ];

        for (const [state, count] of [...mappedStates.entries()].sort(([a], [b]) => a.localeCompare(b))) {
            segments.push({
                value: state,
                label: state.charAt(0).toUpperCase() + state.slice(1),
                badge: count,
            });
        }

        return segments;
    }, [tasks]);

    return (
        <SegmentedControl
            segments={segments}
            value={active ?? ALL}
            onChange={(value) => onChange(value === ALL ? null : value)}
        />
    );
}
