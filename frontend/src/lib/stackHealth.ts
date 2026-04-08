export type StackHealthStatus = "healthy" | "warning" | "critical";

/**
 * Derive the health status of a stack from its task state counts.
 */
export function stackHealth(
  tasksByState: Record<string, number>,
  desiredTasks: number,
): StackHealthStatus {
  const running = tasksByState["running"] ?? 0;

  if (running < desiredTasks) {
    if ((tasksByState["failed"] ?? 0) > 0 || (tasksByState["rejected"] ?? 0) > 0) {
      return "critical";
    }

    return "warning";
  }

  return "healthy";
}
