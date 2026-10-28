import { buildTimeline } from "./taskTimeline";
import type { Task } from "@/api/types";
import { describe, expect, it } from "vitest";

const minute = 60_000;
const now = Date.parse("2026-10-28T12:00:00Z");
const timeWindow = { start: now - 60 * minute, end: now };

function at(minutesAgo: number): string {
  return new Date(now - minutesAgo * minute).toISOString();
}

function makeTask(
  overrides: Partial<Task> & { state?: string; createdAgo: number; changedAgo: number },
): Task {
  const { state = "running", createdAgo, changedAgo, ...rest } = overrides;

  return {
    ID: "task",
    Version: { Index: 1 },
    CreatedAt: at(createdAgo),
    ServiceID: "service-a",
    ServiceName: "web",
    NodeID: "node-a",
    NodeHostname: "worker-1",
    Slot: 1,
    Status: { Timestamp: at(changedAgo), State: state },
    DesiredState: "running",
    Spec: {},
    ...rest,
  };
}

describe("buildTimeline", () => {
  it("groups a replicated service's tasks into one row per slot, in slot order", () => {
    const { rows } = buildTimeline(
      [
        makeTask({ ID: "b", Slot: 10, createdAgo: 30, changedAgo: 29 }),
        makeTask({ ID: "a", Slot: 2, createdAgo: 30, changedAgo: 29 }),
        makeTask({ ID: "c", Slot: 2, state: "shutdown", createdAgo: 50, changedAgo: 30 }),
      ],
      "service",
      timeWindow,
      now,
    );

    expect(rows.map(({ key, label }) => ({ key, label }))).toEqual([
      { key: "slot:2", label: "Replica #2" },
      { key: "slot:10", label: "Replica #10" },
    ]);
    expect(rows[0]?.bars.map(({ task }) => task.ID)).toEqual(["c", "a"]);
  });

  it("groups a global service's tasks by node, named by hostname", () => {
    const { rows } = buildTimeline(
      [
        makeTask({
          ID: "a",
          Slot: 0,
          NodeID: "node-b",
          NodeHostname: "worker-2",
          createdAgo: 5,
          changedAgo: 4,
        }),
        makeTask({
          ID: "b",
          Slot: 0,
          NodeID: "node-a",
          NodeHostname: "worker-1",
          createdAgo: 5,
          changedAgo: 4,
        }),
        makeTask({
          ID: "c",
          Slot: 0,
          NodeID: "abcdef0123456789",
          NodeHostname: undefined,
          createdAgo: 5,
          changedAgo: 4,
        }),
      ],
      "service",
      timeWindow,
      now,
    );

    expect(rows.map(({ key, label }) => ({ key, label }))).toEqual([
      { key: "node:abcdef0123456789", label: "abcdef012345" },
      { key: "node:node-a", label: "worker-1" },
      { key: "node:node-b", label: "worker-2" },
    ]);
  });

  it("groups a node's tasks by service and slot", () => {
    const { rows } = buildTimeline(
      [
        makeTask({
          ID: "a",
          ServiceID: "s-web",
          ServiceName: "web",
          Slot: 2,
          createdAgo: 5,
          changedAgo: 4,
        }),
        makeTask({
          ID: "b",
          ServiceID: "s-web",
          ServiceName: "web",
          Slot: 1,
          createdAgo: 5,
          changedAgo: 4,
        }),
        makeTask({
          ID: "c",
          ServiceID: "s-api",
          ServiceName: "api",
          Slot: 1,
          createdAgo: 5,
          changedAgo: 4,
        }),
        makeTask({
          ID: "d",
          ServiceID: "s-agent",
          ServiceName: "agent",
          Slot: 0,
          createdAgo: 5,
          changedAgo: 4,
        }),
      ],
      "node",
      timeWindow,
      now,
    );

    expect(rows.map(({ key, label }) => ({ key, label }))).toEqual([
      { key: "s-agent", label: "agent" },
      { key: "s-api:1", label: "api #1" },
      { key: "s-web:1", label: "web #1" },
      { key: "s-web:2", label: "web #2" },
    ]);
  });

  it("runs a bar from creation to the last state change once the task has stopped", () => {
    const { rows } = buildTimeline(
      [makeTask({ state: "complete", createdAgo: 40, changedAgo: 10 })],
      "service",
      timeWindow,
      now,
    );

    expect(rows[0]?.bars[0]).toMatchObject({
      start: now - 40 * minute,
      end: now - 10 * minute,
      ongoing: false,
    });
  });

  it("runs a bar to now while the task has not stopped", () => {
    for (const state of ["running", "pending", "starting"]) {
      const { rows } = buildTimeline(
        [makeTask({ state, createdAgo: 40, changedAgo: 39 })],
        "service",
        timeWindow,
        now,
      );

      expect(rows[0]?.bars[0]).toMatchObject({ end: now, ongoing: true });
    }
  });

  it("marks a failed or rejected task and carries its error", () => {
    const failed = makeTask({ ID: "a", state: "failed", createdAgo: 40, changedAgo: 30 });
    failed.Status.Err = "task: non-zero exit (137)";

    const { rows } = buildTimeline(
      [
        failed,
        makeTask({ ID: "b", Slot: 2, state: "rejected", createdAgo: 40, changedAgo: 39 }),
        makeTask({ ID: "c", Slot: 3, state: "shutdown", createdAgo: 40, changedAgo: 39 }),
      ],
      "service",
      timeWindow,
      now,
    );

    expect(rows.map(({ bars }) => bars[0]?.failed)).toEqual([true, true, false]);
    expect(rows[0]?.bars[0]?.error).toBe("task: non-zero exit (137)");
  });

  it("reports a non-zero exit as the error when the task carries none", () => {
    const task = makeTask({ state: "failed", createdAgo: 40, changedAgo: 30 });
    task.Status.ContainerStatus = { ContainerID: "c", ExitCode: 1 };

    const { rows } = buildTimeline([task], "service", timeWindow, now);

    expect(rows[0]?.bars[0]?.error).toBe("exit 1");
  });

  it("leaves out tasks entirely outside the timeWindow, and rows left empty by that", () => {
    const { rows } = buildTimeline(
      [
        makeTask({ ID: "old", Slot: 1, state: "shutdown", createdAgo: 200, changedAgo: 90 }),
        makeTask({ ID: "kept", Slot: 1, state: "shutdown", createdAgo: 200, changedAgo: 30 }),
        makeTask({ ID: "gone", Slot: 2, state: "shutdown", createdAgo: 300, changedAgo: 120 }),
      ],
      "service",
      timeWindow,
      now,
    );

    expect(rows.map(({ bars }) => bars.map(({ task }) => task.ID))).toEqual([["kept"]]);
  });

  it("leaves out tasks created after a timeWindow in the past", () => {
    const past = { start: now - 120 * minute, end: now - 60 * minute };
    const { rows } = buildTimeline(
      [makeTask({ createdAgo: 30, changedAgo: 29 })],
      "service",
      past,
      now,
    );

    expect(rows).toEqual([]);
  });

  it("reports the earliest retained task, inside the timeWindow or not", () => {
    const { earliest } = buildTimeline(
      [
        makeTask({ ID: "a", state: "shutdown", createdAgo: 200, changedAgo: 90 }),
        makeTask({ ID: "b", createdAgo: 30, changedAgo: 29 }),
      ],
      "service",
      timeWindow,
      now,
    );

    expect(earliest).toBe(now - 200 * minute);
    expect(buildTimeline([], "service", timeWindow, now).earliest).toBeUndefined();
  });

  it("starts a task without a creation time at its last state change", () => {
    const task = makeTask({ state: "running", createdAgo: 0, changedAgo: 20 });
    task.CreatedAt = "0001-01-01T00:00:00Z";

    const { rows } = buildTimeline([task], "service", timeWindow, now);

    expect(rows[0]?.bars[0]?.start).toBe(now - 20 * minute);
  });
});
