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

/** How the finished tasks of a job's current run ended. */
export interface JobOutcome {
  completed: number;
  failed: number;
}

/**
 * The one rule every service health indicator applies. Without a target, only
 * nothing running is a verdict: some tasks running may still be a shortfall.
 * A job's failed task is a warning, critical once nothing runs or completed.
 */
export function serviceHealth(
  running: number,
  desired: number | undefined,
  outcome?: JobOutcome | undefined,
): ServiceHealth {
  if (outcome && outcome.failed > 0) {
    return running === 0 && outcome.completed === 0 ? "critical" : "warning";
  }

  if (desired === undefined) {
    return running === 0 ? "critical" : "unknown";
  }

  if (running >= desired) {
    return "healthy";
  }

  return running === 0 ? "critical" : "warning";
}
