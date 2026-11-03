/**
 * The event types that name a swarm resource. A gap, an annotation, and any
 * kind a later server adds name none, so they move no resource's data.
 */
const resourceEventTypes: ReadonlySet<string> = new Set([
  "node",
  "service",
  "task",
  "config",
  "secret",
  "network",
  "volume",
  "stack",
]);

/**
 * Whether an event changed a resource, rather than only a timeline.
 */
export function isResourceEvent({ type }: { type: string }): boolean {
  return resourceEventTypes.has(type);
}
