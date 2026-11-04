/**
 * The event types that name a swarm resource. A gap, an annotation, and any
 * kind a later server adds name none, so they move no resource's data.
 */
export const resourceEventTypes = [
  "node",
  "service",
  "task",
  "config",
  "secret",
  "network",
  "volume",
  "stack",
] as const;

const resourceTypes: ReadonlySet<string> = new Set(resourceEventTypes);

const annotatableTypes: ReadonlySet<string> = new Set(["node", "service", "stack"]);

/**
 * Whether an event may change what a note shows: its own, or a resource it can
 * name coming or going, which marks that resource removed or clears the mark.
 * A stack is keyed by name, so a redeploy brings back the one a note named.
 */
export function changesAnnotations({ type, action }: { type: string; action: string }): boolean {
  return (
    type === "annotation" ||
    ((action === "create" || action === "remove") && annotatableTypes.has(type))
  );
}

/**
 * Whether an event may have moved resources: one of theirs, or a sync, after
 * which any of them may have changed.
 */
export function movesResources({ type }: { type: string }): boolean {
  return type === "sync" || resourceTypes.has(type);
}
