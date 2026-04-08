import type { SearchResourceType, SearchResult } from "../api/types";

export { statusColor } from "./statusColor";

export const typeOrder: SearchResourceType[] = [
  "services",
  "stacks",
  "nodes",
  "tasks",
  "configs",
  "secrets",
  "networks",
  "volumes",
];

export const typeLabels: Record<SearchResourceType, string> = {
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

/**
 * Split "stack_name" into { prefix: "stack", name: "name" }, or null prefix if no underscore.
 */
export function splitStackPrefix(name: string): { prefix: string | null; name: string } {
  const index = name.indexOf("_");

  if (index > 0) {
    return { prefix: name.slice(0, index), name: name.slice(index + 1) };
  }

  return { prefix: null, name };
}

export interface FlatSearchItem {
  type: SearchResourceType;
  result: SearchResult;
}

/**
 * Flatten a SearchResponse into an ordered list of items,
 * respecting typeOrder for consistent display.
 */
export function flattenSearchResults(
  response: { results: Partial<Record<SearchResourceType, SearchResult[]>> },
  filterType?: SearchResourceType,
): FlatSearchItem[] {
  const items: FlatSearchItem[] = [];
  const types = filterType ? [filterType] : typeOrder;

  for (const type of types) {
    const results = response.results[type];

    if (results) {
      for (const result of results) {
        items.push({ type, result });
      }
    }
  }

  return items;
}
