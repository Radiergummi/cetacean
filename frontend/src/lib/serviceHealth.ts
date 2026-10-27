import type { Service } from "@/api/types";

export type ServiceHealth = "healthy" | "warning" | "critical" | "unknown";

/**
 * How many tasks a service asks to have running. A job asks for none, since
 * its tasks run to completion. A global service's target depends on the nodes
 * it is eligible for, which the browser cannot see, so it is unknown.
 */
export function desiredTasks(mode: Service["Spec"]["Mode"]): number | undefined {
  if (mode.ReplicatedJob || mode.GlobalJob) {
    return 0;
  }

  if (mode.Replicated) {
    return mode.Replicated.Replicas ?? 1;
  }

  return undefined;
}

/**
 * The one rule every service health indicator applies. Without a target, only
 * nothing running is a verdict: some tasks running may still be a shortfall.
 */
export function serviceHealth(running: number, desired: number | undefined): ServiceHealth {
  if (desired === undefined) {
    return running === 0 ? "critical" : "unknown";
  }

  if (running >= desired) {
    return "healthy";
  }

  return running === 0 ? "critical" : "warning";
}
