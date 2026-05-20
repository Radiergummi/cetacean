/**
 * Swarm task states in which the container has actually stopped. Mirrors
 * `IsFailureState` plus the natural-completion states on the backend
 * (`internal/cache/restarts.go`). ExitCode is only meaningful here — for
 * non-terminal states Docker often reports a placeholder value like -1.
 */
const terminalTaskStates = new Set([
  "complete",
  "failed",
  "shutdown",
  "rejected",
  "orphaned",
  "remove",
]);

export function isTerminalTaskState(state: string | undefined): boolean {
  return state != null && terminalTaskStates.has(state);
}
