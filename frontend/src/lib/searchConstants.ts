import type { SearchResourceType } from "../api/types";

export const STATE_COLORS: Record<string, string> = {
  running: "bg-green-500",
  ready: "bg-green-500",
  complete: "bg-gray-400",
  shutdown: "bg-gray-400",
  failed: "bg-red-500",
  rejected: "bg-red-500",
  preparing: "bg-yellow-500",
  starting: "bg-yellow-500",
  pending: "bg-yellow-500",
  updating: "updating",
};

export const TYPE_ORDER: SearchResourceType[] = [
  "services",
  "stacks",
  "nodes",
  "tasks",
  "configs",
  "secrets",
  "networks",
  "volumes",
];

export const TYPE_LABELS: Record<SearchResourceType, string> = {
  services: "Services",
  stacks: "Stacks",
  nodes: "Nodes",
  tasks: "Tasks",
  configs: "Configs",
  secrets: "Secrets",
  networks: "Networks",
  volumes: "Volumes",
};

export function resourcePath(type: SearchResourceType, id: string): string {
  return `/${type}/${id}`;
}

/** Split "stack_name" into { prefix: "stack", name: "name" }, or null prefix if no underscore. */
export function splitStackPrefix(name: string): { prefix: string | null; name: string } {
  const i = name.indexOf("_");
  if (i > 0) return { prefix: name.slice(0, i), name: name.slice(i + 1) };
  return { prefix: null, name };
}
