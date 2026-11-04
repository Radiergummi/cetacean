import { changesAnnotations } from "./resourceEvents";
import { describe, expect, it } from "vitest";

describe("changesAnnotations", () => {
  it("counts a note's own events and a resource a note can name coming or going", () => {
    expect(changesAnnotations({ type: "annotation", action: "update" })).toBe(true);
    expect(changesAnnotations({ type: "service", action: "remove" })).toBe(true);
    expect(changesAnnotations({ type: "node", action: "remove" })).toBe(true);
    expect(changesAnnotations({ type: "stack", action: "remove" })).toBe(true);
    expect(changesAnnotations({ type: "service", action: "create" })).toBe(true);
  });

  it("ignores a task's churn and an update to a resource", () => {
    expect(changesAnnotations({ type: "task", action: "remove" })).toBe(false);
    expect(changesAnnotations({ type: "task", action: "update" })).toBe(false);
    expect(changesAnnotations({ type: "service", action: "update" })).toBe(false);
    expect(changesAnnotations({ type: "gap", action: "create" })).toBe(false);
  });
});
