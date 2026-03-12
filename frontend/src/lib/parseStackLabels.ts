const STACK_NAMESPACE_LABEL = "com.docker.stack.namespace";

export function parseStackLabels(labels: Record<string, string> | undefined) {
  const entries = Object.entries(labels || {}).filter(([k]) => k !== STACK_NAMESPACE_LABEL);
  const stack = labels?.[STACK_NAMESPACE_LABEL];
  return { entries, stack };
}
