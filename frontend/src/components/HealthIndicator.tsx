import { serviceHealth } from "@/lib/serviceHealth";
import type { ServiceHealth, TaskCount } from "@/lib/serviceHealth";

type HealthDotProps = { health: ServiceHealth } | TaskCount;

/**
 * Colored dot indicating health state: green (healthy), yellow (warning), red (critical).
 */
export function HealthDot(props: HealthDotProps) {
  const health: ServiceHealth =
    "health" in props ? props.health : serviceHealth(props.running, props.desired, props.outcome);

  return (
    <span
      role="img"
      aria-label={health}
      data-health={health}
      className="inline-block size-2.5 shrink-0 rounded-full bg-status-warning data-[health=critical]:bg-status-danger data-[health=healthy]:bg-status-ok"
    />
  );
}

/** The running count alone, in the softer tone a stack's own pages use. */
export function TaskHealth({ running, desired, outcome }: TaskCount) {
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
 * Displays "running/desired".
 */
export function ReplicaHealth({ running, desired, outcome }: TaskCount) {
  return (
    <span
      data-health={serviceHealth(running, desired, outcome)}
      className="font-medium tabular-nums data-[health=critical]:text-status-danger data-[health=healthy]:text-status-ok data-[health=warning]:text-status-warning"
    >
      {running}/{desired}
    </span>
  );
}
