import type {
  Node,
  Service,
  Task,
  Stack,
  StackDetail,
  Config,
  Secret,
  Network,
  Volume,
} from "./types";

const BASE = "/api";

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

export interface ClusterSnapshot {
  nodeCount: number;
  serviceCount: number;
  taskCount: number;
  stackCount: number;
  tasksByState: Record<string, number>;
  nodesReady: number;
  nodesDown: number;
}

export const api = {
  cluster: () => fetchJSON<ClusterSnapshot>("/cluster"),
  nodes: () => fetchJSON<Node[]>("/nodes"),
  node: (id: string) => fetchJSON<Node>(`/nodes/${id}`),
  services: () => fetchJSON<Service[]>("/services"),
  service: (id: string) => fetchJSON<Service>(`/services/${id}`),
  tasks: () => fetchJSON<Task[]>("/tasks"),
  stacks: () => fetchJSON<Stack[]>("/stacks"),
  stack: (name: string) => fetchJSON<StackDetail>(`/stacks/${name}`),
  configs: () => fetchJSON<Config[]>("/configs"),
  secrets: () => fetchJSON<Secret[]>("/secrets"),
  networks: () => fetchJSON<Network[]>("/networks"),
  volumes: () => fetchJSON<Volume[]>("/volumes"),
  task: (id: string) => fetchJSON<Task>(`/tasks/${id}`),
  taskLogs: (id: string, tail?: number, since?: string, until?: string) => {
    const params = new URLSearchParams({ tail: String(tail || 200) });
    if (since) params.set("since", since);
    if (until) params.set("until", until);
    return fetch(`${BASE}/tasks/${id}/logs?${params}`).then((r) => {
      if (!r.ok) throw new Error(`${r.status} ${r.statusText}`);
      return r.text();
    });
  },
  taskLogsStream: (id: string, opts: { tail?: number; since?: string; signal?: AbortSignal }) => {
    const params = new URLSearchParams({ follow: "true", tail: String(opts.tail ?? 0) });
    if (opts.since) params.set("since", opts.since);
    return fetch(`${BASE}/tasks/${id}/logs?${params}`, { signal: opts.signal });
  },
  serviceTasks: (id: string) => fetchJSON<Task[]>(`/services/${id}/tasks`),
  serviceLogs: (id: string, tail?: number, since?: string, until?: string) => {
    const params = new URLSearchParams({ tail: String(tail || 200) });
    if (since) params.set("since", since);
    if (until) params.set("until", until);
    return fetch(`${BASE}/services/${id}/logs?${params}`).then((r) => {
      if (!r.ok) throw new Error(`${r.status} ${r.statusText}`);
      return r.text();
    });
  },
  serviceLogsStream: (
    id: string,
    opts: { tail?: number; since?: string; signal?: AbortSignal },
  ) => {
    const params = new URLSearchParams({ follow: "true", tail: String(opts.tail ?? 0) });
    if (opts.since) params.set("since", opts.since);
    return fetch(`${BASE}/services/${id}/logs?${params}`, { signal: opts.signal });
  },
  nodeTasks: (id: string) => fetchJSON<Task[]>(`/nodes/${id}/tasks`),
  metricsQuery: (query: string, time?: string) => {
    const params = new URLSearchParams({ query });
    if (time) params.set("time", time);
    return fetchJSON<any>(`/metrics/query?${params}`);
  },
  metricsQueryRange: (query: string, start: string, end: string, step: string) => {
    const params = new URLSearchParams({ query, start, end, step });
    return fetchJSON<any>(`/metrics/query_range?${params}`);
  },
};
