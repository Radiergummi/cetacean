import type {
  Node,
  Service,
  ServiceListItem,
  Task,
  Stack,
  StackDetail,
  Config,
  Secret,
  Network,
  Volume,
  ConfigDetail,
  SecretDetail,
  NetworkDetail,
  VolumeDetail,
  PagedResponse,
  HistoryEntry,
  NetworkTopology,
  PlacementTopology,
  NotificationRuleStatus,
  StackSummary,
  SearchResponse,
} from "./types";

const BASE = "/api";

async function fetchJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const url = `${BASE}${path}`;
  const res = signal ? await fetch(url, { signal }) : await fetch(url);
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error) message = body.error;
    } catch {
      // response wasn't JSON, use status text
    }
    throw new Error(message);
  }
  return res.json();
}

export interface LogLine {
  timestamp: string;
  message: string;
  stream: "stdout" | "stderr";
  attrs?: Record<string, string>;
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
  nodesDraining: number;
  servicesConverged: number;
  servicesDegraded: number;
  reservedCPU: number;
  reservedMemory: number;
  totalCPU: number;
  totalMemory: number;
  prometheusConfigured: boolean;
}

export interface ClusterMetrics {
  cpu: { used: number; total: number; percent: number };
  memory: { used: number; total: number; percent: number };
  disk: { used: number; total: number; percent: number };
}

export interface ListParams {
  limit?: number;
  offset?: number;
  sort?: string;
  dir?: "asc" | "desc";
  search?: string;
}

function buildListURL(path: string, params?: ListParams): string {
  const qs = new URLSearchParams();
  if (params?.limit) qs.set("limit", String(params.limit));
  if (params?.offset) qs.set("offset", String(params.offset));
  if (params?.sort) qs.set("sort", params.sort);
  if (params?.dir) qs.set("dir", params.dir);
  if (params?.search) qs.set("search", params.search);
  const query = qs.toString();
  return `${path}${query ? `?${query}` : ""}`;
}

export const api = {
  cluster: () => fetchJSON<ClusterSnapshot>("/cluster"),
  clusterMetrics: () => fetchJSON<ClusterMetrics>("/cluster/metrics"),
  nodes: (params?: ListParams) => fetchJSON<PagedResponse<Node>>(buildListURL("/nodes", params)),
  node: (id: string) => fetchJSON<Node>(`/nodes/${id}`),
  services: (params?: ListParams) =>
    fetchJSON<PagedResponse<ServiceListItem>>(buildListURL("/services", params)),
  service: (id: string) => fetchJSON<Service>(`/services/${id}`),
  tasks: (params?: ListParams) => fetchJSON<PagedResponse<Task>>(buildListURL("/tasks", params)),
  stacks: (params?: ListParams) => fetchJSON<PagedResponse<Stack>>(buildListURL("/stacks", params)),
  stacksSummary: () => fetchJSON<StackSummary[]>("/stacks/summary"),
  stack: (name: string) => fetchJSON<StackDetail>(`/stacks/${name}`),
  configs: (params?: ListParams) =>
    fetchJSON<PagedResponse<Config>>(buildListURL("/configs", params)),
  config: (id: string) => fetchJSON<ConfigDetail>(`/configs/${id}`),
  secrets: (params?: ListParams) =>
    fetchJSON<PagedResponse<Secret>>(buildListURL("/secrets", params)),
  secret: (id: string) => fetchJSON<SecretDetail>(`/secrets/${id}`),
  networks: (params?: ListParams) =>
    fetchJSON<PagedResponse<Network>>(buildListURL("/networks", params)),
  network: (id: string) => fetchJSON<NetworkDetail>(`/networks/${id}`),
  volumes: (params?: ListParams) =>
    fetchJSON<PagedResponse<Volume>>(buildListURL("/volumes", params)),
  volume: (name: string) => fetchJSON<VolumeDetail>(`/volumes/${name}`),
  task: (id: string) => fetchJSON<Task>(`/tasks/${id}`),
  taskLogs: (
    id: string,
    opts?: {
      limit?: number;
      after?: string;
      before?: string;
      stream?: string;
      signal?: AbortSignal;
    },
  ) => {
    const params = new URLSearchParams({ limit: String(opts?.limit || 500) });
    if (opts?.after) params.set("after", opts.after);
    if (opts?.before) params.set("before", opts.before);
    if (opts?.stream) params.set("stream", opts.stream);
    return fetchJSON<LogResponse>(`/tasks/${id}/logs?${params}`, opts?.signal);
  },
  serviceTasks: (id: string) => fetchJSON<Task[]>(`/services/${id}/tasks`),
  serviceLogs: (
    id: string,
    opts?: {
      limit?: number;
      after?: string;
      before?: string;
      stream?: string;
      signal?: AbortSignal;
    },
  ) => {
    const params = new URLSearchParams({ limit: String(opts?.limit || 500) });
    if (opts?.after) params.set("after", opts.after);
    if (opts?.before) params.set("before", opts.before);
    if (opts?.stream) params.set("stream", opts.stream);
    return fetchJSON<LogResponse>(`/services/${id}/logs?${params}`, opts?.signal);
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
  history: (params?: { type?: string; resourceId?: string; limit?: number }) => {
    const qs = new URLSearchParams();
    if (params?.type) qs.set("type", params.type);
    if (params?.resourceId) qs.set("resourceId", params.resourceId);
    if (params?.limit) qs.set("limit", String(params.limit));
    const query = qs.toString();
    return fetchJSON<HistoryEntry[]>(`/history${query ? `?${query}` : ""}`);
  },
  topologyNetworks: () => fetchJSON<NetworkTopology>("/topology/networks"),
  topologyPlacement: () => fetchJSON<PlacementTopology>("/topology/placement"),
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
  notificationRules: () => fetchJSON<NotificationRuleStatus[]>("/notifications/rules"),
  search: (q: string, limit?: number) =>
    fetchJSON<SearchResponse>(
      `/search?q=${encodeURIComponent(q)}${limit !== undefined ? `&limit=${limit}` : ""}`
    ),
};
