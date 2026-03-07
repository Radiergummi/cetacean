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

export interface LogLine {
  timestamp: string;
  message: string;
  stream: "stdout" | "stderr";
}

export interface LogResponse {
  lines: LogLine[];
  oldest: string;
  newest: string;
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
  taskLogs: (
    id: string,
    opts?: { limit?: number; after?: string; before?: string; stream?: string },
  ) => {
    const params = new URLSearchParams({ limit: String(opts?.limit || 500) });
    if (opts?.after) params.set("after", opts.after);
    if (opts?.before) params.set("before", opts.before);
    if (opts?.stream) params.set("stream", opts.stream);
    return fetchJSON<LogResponse>(`/tasks/${id}/logs?${params}`);
  },
  serviceTasks: (id: string) => fetchJSON<Task[]>(`/services/${id}/tasks`),
  serviceLogs: (
    id: string,
    opts?: { limit?: number; after?: string; before?: string; stream?: string },
  ) => {
    const params = new URLSearchParams({ limit: String(opts?.limit || 500) });
    if (opts?.after) params.set("after", opts.after);
    if (opts?.before) params.set("before", opts.before);
    if (opts?.stream) params.set("stream", opts.stream);
    return fetchJSON<LogResponse>(`/services/${id}/logs?${params}`);
  },
  serviceLogsStreamURL: (id: string, opts?: { after?: string; stream?: string }) => {
    const params = new URLSearchParams();
    if (opts?.after) params.set("after", opts.after);
    if (opts?.stream) params.set("stream", opts.stream);
    const qs = params.toString();
    return `${BASE}/services/${id}/logs${qs ? `?${qs}` : ""}`;
  },
  taskLogsStreamURL: (id: string, opts?: { after?: string; stream?: string }) => {
    const params = new URLSearchParams();
    if (opts?.after) params.set("after", opts.after);
    if (opts?.stream) params.set("stream", opts.stream);
    const qs = params.toString();
    return `${BASE}/tasks/${id}/logs${qs ? `?${qs}` : ""}`;
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
