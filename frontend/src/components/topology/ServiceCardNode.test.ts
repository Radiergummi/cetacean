import { replicaStatus } from "./ServiceCardNode";
import { describe, it, expect } from "vitest";

describe("replicaStatus", () => {
  it("is healthy only when every desired replica is up", () => {
    expect(replicaStatus("replicated", 3, 3)).toEqual({ label: "3/3", tone: "bg-status-ok" });
    expect(replicaStatus("replicated", 2, 3)).toEqual({ label: "2/3", tone: "bg-status-warning" });
    expect(replicaStatus("replicated", 0, 1)).toEqual({ label: "0/1", tone: "bg-status-danger" });
  });

  it("describes a global service by what is running", () => {
    // Docker publishes no desired count for a global service, so measuring
    // against one would render a healthy service as "1/0".
    expect(replicaStatus("global", 1, 0)).toEqual({ label: "1 running", tone: "bg-status-ok" });
    expect(replicaStatus("global", 0, 0)).toEqual({
      label: "0 running",
      tone: "bg-status-danger",
    });
  });
});
