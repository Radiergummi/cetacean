export const stackNamespaceLabel = "com.docker.stack.namespace";

export function parseStackLabels(labels: Record<string, string> | null | undefined) {
  const entries = Object.entries(labels ?? {}).filter(([key]) => key !== stackNamespaceLabel);
  const stack = labels?.[stackNamespaceLabel];

  return { entries, stack };
}

/**
 * Drops the "<stack>_" prefix Docker gives a stack's resources, so the name
 * doesn't repeat the stack it is shown next to. A resource can join a stack by
 * label without being named for it, so the prefix is only removed when it is
 * actually there — and never guessed at when the resource belongs to no stack,
 * where an underscore is just part of the name.
 */
export function stripStackPrefix(name: string, stack?: string | undefined): string {
  if (stack && name.startsWith(`${stack}_`)) {
    return name.slice(stack.length + 1);
  }

  return name;
}
