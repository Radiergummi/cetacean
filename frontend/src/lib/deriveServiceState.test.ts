import { serviceUpdateStatus } from "./deriveServiceState";
import { describe, expect, it } from "vitest";

describe("serviceUpdateStatus", () => {
  it("returns stable when UpdateStatus is undefined", () => {
    expect(serviceUpdateStatus({})).toEqual({ label: "Stable", state: "stable" });
  });

  it("returns stable when state is completed", () => {
    expect(serviceUpdateStatus({ UpdateStatus: { State: "completed" } })).toEqual({
      label: "Stable",
      state: "stable",
    });
  });

  it("returns Updating for updating state", () => {
    expect(serviceUpdateStatus({ UpdateStatus: { State: "updating" } })).toEqual({
      label: "Updating",
      state: "updating",
    });
  });

  it("returns Paused for paused state", () => {
    expect(serviceUpdateStatus({ UpdateStatus: { State: "paused" } })).toEqual({
      label: "Paused",
      state: "paused",
    });
  });

  it("returns Rolling back for rollback_started state", () => {
    expect(serviceUpdateStatus({ UpdateStatus: { State: "rollback_started" } })).toEqual({
      label: "Rolling back",
      state: "rollback_started",
    });
  });

  it("returns Rolled back for rollback_completed state", () => {
    expect(serviceUpdateStatus({ UpdateStatus: { State: "rollback_completed" } })).toEqual({
      label: "Rolled back",
      state: "rollback_completed",
    });
  });

  it("returns raw state as label for unknown states", () => {
    expect(serviceUpdateStatus({ UpdateStatus: { State: "some_new_state" } })).toEqual({
      label: "some_new_state",
      state: "some_new_state",
    });
  });
});
