import { changesAnnotations } from "./resourceEvents";
import { describe, expect, it } from "vitest";

describe("changesAnnotations", () => {
  it("counts a note's own events and the removal of a resource a note can name", () => {
    expect(changesAnnotations({ type: "annotation", action: "update" })).toBe(true);
    expect(changesAnnotations({ type: "service", action: "remove" })).toBe(true);
    expect(changesAnnotations({ type: "node", action: "remove" })).toBe(true);
    expect(changesAnnotations({ type: "stack", action: "remove" })).toBe(true);
  });

  it("ignores a task's churn and any change short of a removal", () => {
    expect(changesAnnotations({ type: "task", action: "remove" })).toBe(false);
    expect(changesAnnotations({ type: "task", action: "update" })).toBe(false);
    expect(changesAnnotations({ type: "service", action: "update" })).toBe(false);
    expect(changesAnnotations({ type: "gap", action: "create" })).toBe(false);
  });
});
