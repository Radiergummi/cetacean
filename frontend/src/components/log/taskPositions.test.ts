import { TaskPositions } from "./taskPositions";
import { describe, expect, it } from "vitest";

function line(task: string, timestamp: string, message = "hello") {
  return { timestamp, message, stream: "stdout" as const, attrs: { taskId: task } };
}

describe("TaskPositions", () => {
  it("resumes from the task furthest behind", () => {
    const positions = new TaskPositions();

    positions.record(line("a", "2024-01-01T00:00:10.000000000Z"));
    positions.record(line("b", "2024-01-01T00:00:02.000000000Z"));

    expect(positions.resumeCursor()).toBe("2024-01-01T00:00:02.000000000Z");
  });

  it("holds back for a quiet task no further than a minute behind the newest line", () => {
    const positions = new TaskPositions();

    positions.record(line("b", "2024-01-01T00:00:00.000000000Z"));
    positions.record(line("a", "2024-01-01T00:05:00.000000000Z"));

    expect(positions.resumeCursor()).toBe("2024-01-01T00:04:00.000000000Z");
  });

  it("has no cursor before any line", () => {
    expect(new TaskPositions().resumeCursor()).toBeUndefined();
  });

  it("recognises a line a resume replays", () => {
    const positions = new TaskPositions();

    positions.record(line("a", "2024-01-01T00:00:05.000000000Z", "five"));

    expect(positions.isReplayed(line("a", "2024-01-01T00:00:04.000000000Z"))).toBe(true);
    expect(positions.isReplayed(line("a", "2024-01-01T00:00:05.000000000Z", "five"))).toBe(true);
    expect(positions.isReplayed(line("a", "2024-01-01T00:00:05.000000000Z", "other"))).toBe(false);
    expect(positions.isReplayed(line("a", "2024-01-01T00:00:06.000000000Z"))).toBe(false);
    expect(positions.isReplayed(line("b", "2024-01-01T00:00:01.000000000Z"))).toBe(false);
  });

  it("never treats a line without a timestamp as replayed", () => {
    const positions = new TaskPositions();

    positions.record(line("a", "2024-01-01T00:00:05.000000000Z"));

    expect(positions.isReplayed(line("a", ""))).toBe(false);
  });
});
