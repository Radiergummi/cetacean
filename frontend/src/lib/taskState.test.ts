import { isTerminalTaskState } from "./taskState";
import { describe, it, expect } from "vitest";

describe("isTerminalTaskState", () => {
  it("recognises the states in which the container has stopped", () => {
    for (const state of ["complete", "failed", "shutdown", "rejected", "orphaned", "remove"]) {
      expect(isTerminalTaskState(state)).toBe(true);
    }
  });

  it("rejects the states a task passes through on its way up", () => {
    for (const state of [
      "new",
      "pending",
      "assigned",
      "accepted",
      "preparing",
      "starting",
      "running",
    ]) {
      expect(isTerminalTaskState(state)).toBe(false);
    }
  });

  it("treats a missing state as non-terminal, so no exit code is shown", () => {
    expect(isTerminalTaskState(undefined)).toBe(false);
    expect(isTerminalTaskState("")).toBe(false);
  });
});
