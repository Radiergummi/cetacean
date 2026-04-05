export type HealthStatus = "healthy" | "warning" | "critical";

type HealthDotProps =
  | { health: HealthStatus }
  | { running: number; desired: number };

/**
 * Colored dot indicating health state: green (healthy), yellow (warning), red (critical).
 *
 * Accepts either an explicit `health` status or `running`/`desired` counts
 * (healthy when running >= desired and desired > 0, otherwise critical).
 */
export function HealthDot(props: HealthDotProps) {
  const health =
    "health" in props
      ? props.health
      : props.running >= props.desired && props.desired > 0
        ? "healthy"
        : "critical";

  return (
    <span
      role="img"
      aria-label={health}
      data-health={health}
      className="inline-block size-2.5 shrink-0 rounded-full bg-yellow-500 data-[health=critical]:bg-red-500 data-[health=healthy]:bg-green-500"
    />
  );
}

/**
 * Displays "running/desired" colored green when healthy, red when not.
 */
export function ReplicaHealth({ running, desired }: { running: number; desired: number }) {
  const healthy = running >= desired && desired > 0;

  return (
    <span
      data-healthy={healthy || undefined}
      className="font-medium text-red-600 tabular-nums data-healthy:text-green-600 dark:text-red-400 dark:data-healthy:text-green-400"
    >
      {running}/{desired}
    </span>
  );
}
