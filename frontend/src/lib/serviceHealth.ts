import type { ServiceListItem } from "@/api/types";

export type ServiceHealth = "healthy" | "warning" | "critical";

/** How the finished tasks of a job's current run ended. */
export interface JobOutcome {
  completed: number;
  failed: number;
}

/** A service's running count against its target, and for a job how its run ended. */
export type TaskCount = {
  running: number;
  desired: number;
  outcome?: JobOutcome | undefined;
};

/**
 * The counts a service row is judged on. A job keeps nothing running, so it is
 * judged on its run's outcome, not on its completion target.
 */
export function serviceTaskCount({
  RunningTasks,
  DesiredTasks,
  CompletedTasks,
  FailedTasks,
  Spec: { Mode },
}: ServiceListItem): TaskCount {
  if (Mode.ReplicatedJob || Mode.GlobalJob) {
    return {
      running: RunningTasks,
      desired: 0,
      outcome: { completed: CompletedTasks ?? 0, failed: FailedTasks ?? 0 },
    };
  }

  return { running: RunningTasks, desired: DesiredTasks };
}

/**
 * The one rule every service health indicator applies. A job's failed task is
 * a warning, critical once nothing runs or completed.
 */
export function serviceHealth(
  running: number,
  desired: number,
  outcome?: JobOutcome | undefined,
): ServiceHealth {
  if (outcome && outcome.failed > 0) {
    return running === 0 && outcome.completed === 0 ? "critical" : "warning";
  }

  if (running >= desired) {
    return "healthy";
  }

  return running === 0 ? "critical" : "warning";
}
