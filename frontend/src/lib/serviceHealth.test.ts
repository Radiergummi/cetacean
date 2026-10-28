import type { ServiceListItem } from "../api/types";
import { serviceHealth, serviceTaskCount } from "./serviceHealth";
import { describe, expect, it } from "vitest";

describe("serviceTaskCount", () => {
  const service = (Mode: ServiceListItem["Spec"]["Mode"], counts: Partial<ServiceListItem>) =>
    ({ ID: "s1", Version: { Index: 1 }, Spec: { Name: "s", Mode }, ...counts }) as ServiceListItem;

  it("counts a replicated service against its replicas", () => {
    expect(
      serviceTaskCount(
        service({ Replicated: { Replicas: 3 } }, { RunningTasks: 2, DesiredTasks: 3 }),
      ),
    ).toEqual({ running: 2, desired: 3 });
  });

  it("counts a global service against its eligible nodes", () => {
    expect(serviceTaskCount(service({ Global: {} }, { RunningTasks: 4, DesiredTasks: 5 }))).toEqual(
      {
        running: 4,
        desired: 5,
      },
    );
  });

  it("judges a job on its current run, with nothing to keep running", () => {
    expect(
      serviceTaskCount(
        service(
          { ReplicatedJob: {} },
          { RunningTasks: 0, DesiredTasks: 2, CompletedTasks: 1, FailedTasks: 1 },
        ),
      ),
    ).toEqual({ running: 0, desired: 0, outcome: { completed: 1, failed: 1 } });
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
