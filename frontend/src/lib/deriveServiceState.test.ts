import {
  containerSpecFromConfig,
  deriveServiceSubResources,
  envMapToSlice,
  serviceModeLabel,
  serviceUpdateStatus,
  withContainerSpec,
} from "./deriveServiceState";
import type { Service } from "@/api/types";
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

describe("serviceModeLabel", () => {
  it("names each of Swarm's four modes", () => {
    expect(serviceModeLabel({ Replicated: { Replicas: 2 } })).toBe("replicated");
    expect(serviceModeLabel({ Global: {} })).toBe("global");
    expect(serviceModeLabel({ ReplicatedJob: { TotalCompletions: 2 } })).toBe("replicated-job");
    expect(serviceModeLabel({ GlobalJob: {} })).toBe("global-job");
  });
});

// A save is written back through these and the page derives from the result,
// so anything they drop would vanish from the editor until the stream answers.
describe("writing sub-resources back onto the service", () => {
  const service = {
    ID: "svc1",
    Version: { Index: 1 },
    Spec: {
      Name: "web",
      Labels: null,
      Mode: {},
      TaskTemplate: {
        ContainerSpec: {
          Image: "nginx",
          Env: ["A=1", "B=x=y"],
          Command: ["nginx"],
          User: "www",
          TTY: true,
          Init: true,
          StopGracePeriod: 10,
          CapabilityAdd: ["NET_ADMIN"],
          DNSConfig: { Nameservers: ["1.1.1.1"] },
        },
      },
    },
  } as Service;

  it("round-trips the container configuration", () => {
    const { containerConfig } = deriveServiceSubResources(service);
    const written = withContainerSpec(service, containerSpecFromConfig(containerConfig));

    expect(deriveServiceSubResources(written).containerConfig).toEqual(containerConfig);
    expect(written.Spec.TaskTemplate?.ContainerSpec?.Image).toBe("nginx");
  });

  it("round-trips the environment", () => {
    const { envVars } = deriveServiceSubResources(service);
    const written = withContainerSpec(service, { Env: envMapToSlice(envVars) });

    expect(deriveServiceSubResources(written).envVars).toEqual({ A: "1", B: "x=y" });
  });
});
