import { replicaHealthColor, statusColor } from "./statusColor";
import { describe, it, expect } from "vitest";

describe("statusColor", () => {
  it("returns green for running", () => {
    expect(statusColor("running")).toBe("bg-green-500");
  });

  it("returns red for failed", () => {
    expect(statusColor("failed")).toBe("bg-red-500");
  });

  it("returns yellow for starting", () => {
    expect(statusColor("starting")).toBe("bg-yellow-500");
  });

  it("returns gray for unknown states", () => {
    expect(statusColor("unknown")).toBe("bg-gray-300 dark:bg-gray-600");
  });
});

describe("replicaHealthColor", () => {
  it("returns green when all replicas running", () => {
    expect(replicaHealthColor(3, 3)).toBe("bg-green-500");
  });

  it("returns yellow when partially running", () => {
    expect(replicaHealthColor(1, 3)).toBe("bg-yellow-500");
  });

  it("returns red when none running", () => {
    expect(replicaHealthColor(0, 3)).toBe("bg-red-500");
  });

  it("returns green for zero desired zero running", () => {
    expect(replicaHealthColor(0, 0)).toBe("bg-green-500");
  });
});
