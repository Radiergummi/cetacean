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

/**
 * Whether an event may have moved resources: one of theirs, or a sync, after
 * which any of them may have changed.
 */
export function movesResources({ type }: { type: string }): boolean {
  return type === "sync" || resourceTypes.has(type);
}
