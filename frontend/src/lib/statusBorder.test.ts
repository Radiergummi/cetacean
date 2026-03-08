import { describe, it, expect } from "vitest";
import { statusBorder } from "./statusBorder";

describe("statusBorder", () => {
  it("returns green for running/ready/complete", () => {
    for (const state of ["running", "ready", "complete"]) {
      expect(statusBorder(state)).toContain("green");
    }
  });

  it("returns red for failed/rejected/down/orphaned", () => {
    for (const state of ["failed", "rejected", "down", "orphaned"]) {
      expect(statusBorder(state)).toContain("red");
    }
  });

  it("returns yellow for pending states", () => {
    for (const state of ["preparing", "starting", "pending", "assigned", "accepted"]) {
      expect(statusBorder(state)).toContain("yellow");
    }
  });

  it("returns gray for shutdown/remove", () => {
    for (const state of ["shutdown", "remove"]) {
      expect(statusBorder(state)).toContain("gray");
    }
  });

  it("returns empty string for unknown state", () => {
    expect(statusBorder("unknown")).toBe("");
  });
});
