import { desiredTasks, serviceHealth } from "./serviceHealth";
import { describe, expect, it } from "vitest";

describe("desiredTasks", () => {
  it("is the replica count of a replicated service", () => {
    expect(desiredTasks({ Replicated: { Replicas: 3 } })).toBe(3);
  });

  it("is zero for a job, whose tasks run to completion", () => {
    expect(desiredTasks({ ReplicatedJob: {} })).toBe(0);
    expect(desiredTasks({ GlobalJob: {} })).toBe(0);
  });

  it("is unknown for a global service", () => {
    expect(desiredTasks({ Global: {} })).toBeUndefined();
  });
});

describe("serviceHealth", () => {
  it("is healthy when every desired task runs", () => {
    expect(serviceHealth(3, 3)).toBe("healthy");
  });

  it("is healthy for a service scaled to zero on purpose", () => {
    expect(serviceHealth(0, 0)).toBe("healthy");
  });

  it("warns on a partial shortfall", () => {
    expect(serviceHealth(1, 3)).toBe("warning");
  });

  it("is critical when nothing runs that should", () => {
    expect(serviceHealth(0, 3)).toBe("critical");
  });

  it("is critical for a global service with nothing running", () => {
    expect(serviceHealth(0, undefined)).toBe("critical");
  });

  it("does not call a running global service healthy without knowing its target", () => {
    expect(serviceHealth(2, undefined)).toBe("unknown");
  });

  it("is critical for a job whose tasks failed and none completed", () => {
    expect(serviceHealth(0, 0, { completed: 0, failed: 3 })).toBe("critical");
  });

  it("is healthy for a job whose run completed without a failure", () => {
    expect(serviceHealth(0, 0, { completed: 2, failed: 0 })).toBe("healthy");
  });

  it("warns for a job whose run both completed and failed tasks", () => {
    expect(serviceHealth(0, 0, { completed: 1, failed: 3 })).toBe("warning");
  });

  it("warns for a job still running after a failure", () => {
    expect(serviceHealth(1, 0, { completed: 0, failed: 1 })).toBe("warning");
  });
});
