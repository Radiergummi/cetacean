import { replicaHealthColor, statusColor, statusTone, toneSurface } from "./statusColor";
import { describe, it, expect } from "vitest";

describe("statusTone", () => {
  it("groups the healthy states", () => {
    for (const state of ["running", "ready", "complete"]) {
      expect(statusTone(state)).toBe("ok");
    }
  });

  it("groups the failure states", () => {
    for (const state of ["failed", "rejected", "down", "orphaned"]) {
      expect(statusTone(state)).toBe("danger");
    }
  });

  it("groups the in-flight states", () => {
    for (const state of ["preparing", "starting", "pending", "assigned", "accepted"]) {
      expect(statusTone(state)).toBe("warning");
    }
  });

  it("falls back to neutral for a state it does not know", () => {
    expect(statusTone("unknown")).toBe("neutral");
    expect(statusTone("shutdown")).toBe("neutral");
  });
});

describe("statusColor", () => {
  it("resolves through the semantic token, not a palette shade", () => {
    expect(statusColor("running")).toBe("bg-status-ok");
    expect(statusColor("failed")).toBe("bg-status-danger");
    expect(statusColor("starting")).toBe("bg-status-warning");
    expect(statusColor("unknown")).toBe("bg-status-neutral");
  });

  // The token carries its own dark value, so no call site pairs a `dark:`
  // variant with it any more.
  it("needs no dark-mode variant", () => {
    for (const state of ["running", "failed", "starting", "unknown"]) {
      expect(statusColor(state)).not.toContain("dark:");
    }
  });
});

describe("replicaHealthColor", () => {
  it("reads all-running as ok", () => {
    expect(replicaHealthColor(3, 3)).toBe("bg-status-ok");
    expect(replicaHealthColor(0, 0)).toBe("bg-status-ok");
  });

  it("reads partial as warning and none as danger", () => {
    expect(replicaHealthColor(1, 3)).toBe("bg-status-warning");
    expect(replicaHealthColor(0, 3)).toBe("bg-status-danger");
  });
});

describe("toneSurface", () => {
  it("tints the same token it colours the text with", () => {
    for (const tone of ["ok", "warning", "danger", "neutral"] as const) {
      expect(toneSurface[tone]).toBe(`bg-status-${tone}/15 text-status-${tone}`);
    }
  });
});
