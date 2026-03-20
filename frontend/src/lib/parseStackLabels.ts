export const stackNamespaceLabel = "com.docker.stack.namespace";

export function parseStackLabels(labels: Record<string, string> | undefined) {
  const entries = Object.entries(labels || {}).filter(([key]) => key !== stackNamespaceLabel);
  const stack = labels?.[stackNamespaceLabel];

  return { entries, stack };
}
