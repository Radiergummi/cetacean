import type { SearchResourceType } from "../api/types";

export { statusColor } from "./statusColor";

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

export function resourcePath(type: string, id: string, name?: string): string | null {
  switch (type) {
    // plural (SearchResourceType)
    case "nodes":
    case "services":
    case "tasks":
    case "configs":
    case "secrets":
    case "networks":
      return `/${type}/${id}`;
    case "volumes":
      return `/volumes/${name ?? id}`;
    case "stacks":
      return `/stacks/${name ?? id}`;
    // singular (HistoryEntry type)
    case "node":
    case "service":
    case "task":
    case "config":
    case "secret":
    case "network":
      return `/${type}s/${id}`;
    case "volume":
      return `/volumes/${name ?? id}`;
    case "stack":
      return `/stacks/${name ?? id}`;
    default:
      return null;
  }
}

/** Split "stack_name" into { prefix: "stack", name: "name" }, or null prefix if no underscore. */
export function splitStackPrefix(name: string): { prefix: string | null; name: string } {
  const i = name.indexOf("_");
  if (i > 0) return { prefix: name.slice(0, i), name: name.slice(i + 1) };
  return { prefix: null, name };
}
