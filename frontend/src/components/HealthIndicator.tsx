import { serviceHealth } from "@/lib/serviceHealth";
import type { JobOutcome, ServiceHealth } from "@/lib/serviceHealth";

export type HealthStatus = "healthy" | "warning" | "critical";

/**
 * A count whose `desired` is absent when the target is unknown, as for a global
 * service; a job's count may carry how its finished tasks ended.
 */
type TaskCountProps = {
  running: number;
  desired?: number | undefined;
  outcome?: JobOutcome | undefined;
};

type HealthDotProps = { health: HealthStatus } | TaskCountProps;

/**
 * Colored dot indicating health state: green (healthy), yellow (warning), red (critical),
 * grey when a running global service's target is unknown.
 */
export function HealthDot(props: HealthDotProps) {
  const health: ServiceHealth =
    "health" in props ? props.health : serviceHealth(props.running, props.desired, props.outcome);

  return (
    <span
      role="img"
      aria-label={health}
      data-health={health}
      className="inline-block size-2.5 shrink-0 rounded-full bg-status-warning data-[health=critical]:bg-status-danger data-[health=healthy]:bg-status-ok data-[health=unknown]:bg-muted-foreground"
    />
  );
}

/** The running count alone, in the softer tone a stack's own pages use. */
export function TaskHealth({ running, desired, outcome }: TaskCountProps) {
  return (
    <span
      data-health={serviceHealth(running, desired, outcome)}
      className="tabular-nums data-[health=critical]:text-status-danger data-[health=healthy]:text-status-ok data-[health=warning]:text-status-warning"
    >
      {running}
    </span>
  );
}

/**
 * Displays "running/desired", or only the running count when the target is unknown.
 */
export function ReplicaHealth({ running, desired, outcome }: TaskCountProps) {
  return (
    <span
      data-health={serviceHealth(running, desired, outcome)}
      className="font-medium tabular-nums data-[health=critical]:text-status-danger data-[health=healthy]:text-status-ok data-[health=warning]:text-status-warning"
    >
      {running}
      {desired !== undefined && `/${desired}`}
    </span>
  );
}
