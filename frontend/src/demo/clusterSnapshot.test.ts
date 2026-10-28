import type { Node, Service, Task } from "../api/types";
import { buildDataset } from "./dataset";
import { buildClusterSnapshot } from "./handlers";
import { describe, expect, it } from "vitest";

const base = buildDataset();

function node(id: string, availability: string): Node {
  const copy = structuredClone(base.nodes[0]!);
  copy.ID = id;
  copy.Status.State = "ready";
  copy.Spec.Availability = availability;

  return copy;
}

function service(id: string, mode: Service["Spec"]["Mode"], cpu = 0): Service {
  const copy = structuredClone(base.services[0]!);
  copy.ID = id;
  copy.Spec.Mode = mode;
  delete copy.UpdateStatus;
  copy.Spec.TaskTemplate!.Resources = cpu > 0 ? { Reservations: { NanoCPUs: cpu } } : {};

  return copy;
}

function task(serviceID: string, state: string, desiredState = "running"): Task {
  const copy = structuredClone(base.tasks[0]!);
  copy.ID = `${serviceID}-${state}-${Math.random()}`;
  copy.ServiceID = serviceID;
  copy.Status.State = state;
  copy.DesiredState = desiredState;

  return copy;
}

function snapshot(nodes: Node[], services: Service[], tasks: Task[]) {
  return buildClusterSnapshot({ ...base, nodes, services, tasks });
}

/**
 * The demo overview counts services by the rule the server uses: running or a
 * completed job is converged; failed, pending or flapping is degraded; a
 * service mid-update is in neither.
 */
describe("demo cluster snapshot", () => {
  it("counts services by their state", () => {
    const rolling = service("rolling", { Replicated: { Replicas: 1 } });
    rolling.UpdateStatus = { State: "updating" };

    const result = snapshot(
      [node("n1", "active")],
      [
        service("web", { Replicated: { Replicas: 2 } }),
        service("short", { Replicated: { Replicas: 2 } }),
        service("job", { ReplicatedJob: {} }),
        rolling,
      ],
      [
        task("web", "running"),
        task("web", "running"),
        task("short", "running"),
        task("job", "complete"),
        task("rolling", "running"),
      ],
    );

    expect(result.servicesConverged).toBe(2);
    expect(result.servicesDegraded).toBe(1);
  });

  it("does not count a global service with nowhere to run as degraded", () => {
    const result = snapshot([node("n1", "drain")], [service("agent", { Global: {} })], []);

    expect(result.servicesConverged).toBe(1);
    expect(result.servicesDegraded).toBe(0);
  });

  it("reserves a global service once per eligible node", () => {
    const result = snapshot(
      [node("n1", "active"), node("n2", "drain")],
      [service("agent", { Global: {} }, 100)],
      [],
    );

    expect(result.reservedCPU).toBe(100);
  });

  it("reserves for a paused node, which keeps its task, but places nothing new there", () => {
    const reserved = snapshot(
      [node("n1", "active"), node("n2", "pause")],
      [service("agent", { Global: {} }, 100)],
      [],
    );
    const pausedOnly = snapshot([node("n1", "pause")], [service("agent", { Global: {} })], []);

    expect(reserved.reservedCPU).toBe(200);
    expect(pausedOnly.servicesConverged).toBe(1);
  });

  it("holds a job to its completion target", () => {
    const result = snapshot(
      [node("n1", "active")],
      [
        service("short", { ReplicatedJob: { TotalCompletions: 2 } }),
        service("done", { ReplicatedJob: { TotalCompletions: 2 } }),
      ],
      [task("short", "complete"), task("done", "complete"), task("done", "complete")],
    );

    expect(result.servicesConverged).toBe(1);
    expect(result.servicesDegraded).toBe(1);
  });

  it("does not count a task being shut down as running", () => {
    const result = snapshot(
      [node("n1", "active")],
      [service("web", { Replicated: { Replicas: 1 } })],
      [task("web", "running", "shutdown")],
    );

    expect(result.servicesDegraded).toBe(1);
  });
});
