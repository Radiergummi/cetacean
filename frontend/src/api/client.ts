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
  CollectionResponse,
  HistoryEntry,
  NetworkTopology,
  PlacementTopology,
  StackSummary,
  SearchResponse,
  SwarmInfo,
  DiskUsageSummary,
  Plugin,
  MonitoringStatus,
  PrometheusResponse,
} from "./types";

const headers = { Accept: "application/json" };

async function fetchJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { headers, signal });
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.detail) message = body.detail;
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
  hasMore: boolean;
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
  localNodeID?: string;
}

export interface ClusterMetrics {
  cpu: { used: number; total: number; percent: number };
  memory: { used: number; total: number; percent: number };
  disk: { used: number; total: number; percent: number };
}

export interface LogOpts {
  limit?: number;
  after?: string;
  before?: string;
  stream?: string;
  signal?: AbortSignal;
}

function buildLogParams(opts?: LogOpts): URLSearchParams {
  const params = new URLSearchParams({ limit: String(opts?.limit || 500) });
  if (opts?.after) params.set("after", opts.after);
  if (opts?.before) params.set("before", opts.before);
  if (opts?.stream) params.set("stream", opts.stream);
  return params;
}

function buildLogStreamURL(path: string, opts?: { after?: string; stream?: string }): string {
  const params = new URLSearchParams();
  if (opts?.after) params.set("after", opts.after);
  if (opts?.stream) params.set("stream", opts.stream);
  const qs = params.toString();
  return `${path}${qs ? `?${qs}` : ""}`;
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
  if (params?.limit != null) qs.set("limit", String(params.limit));
  if (params?.offset != null) qs.set("offset", String(params.offset));
  if (params?.sort) qs.set("sort", params.sort);
  if (params?.dir) qs.set("dir", params.dir);
  if (params?.search) qs.set("search", params.search);
  const query = qs.toString();
  return `${path}${query ? `?${query}` : ""}`;
}

export const api = {
  cluster: () => fetchJSON<ClusterSnapshot>("/cluster"),
  swarm: () => fetchJSON<SwarmInfo>("/swarm"),
  plugins: () => fetchJSON<CollectionResponse<Plugin>>("/plugins").then((r) => r.items),
  clusterMetrics: () => fetchJSON<ClusterMetrics>("/cluster/metrics"),
  monitoringStatus: () => fetchJSON<MonitoringStatus>("/-/metrics/status"),
  nodes: (params?: ListParams) => fetchJSON<PagedResponse<Node>>(buildListURL("/nodes", params)),
  node: (id: string) => fetchJSON<{ node: Node }>(`/nodes/${id}`).then((r) => r.node),
  services: (params?: ListParams) =>
    fetchJSON<PagedResponse<ServiceListItem>>(buildListURL("/services", params)),
  service: (id: string) =>
    fetchJSON<{ service: Service }>(`/services/${id}`).then((r) => r.service),
  tasks: (params?: ListParams) => fetchJSON<PagedResponse<Task>>(buildListURL("/tasks", params)),
  stacks: (params?: ListParams) => fetchJSON<PagedResponse<Stack>>(buildListURL("/stacks", params)),
  stacksSummary: () =>
    fetchJSON<CollectionResponse<StackSummary>>("/stacks/summary").then((r) => r.items),
  stack: (name: string) =>
    fetchJSON<{ stack: StackDetail }>(`/stacks/${name}`).then((r) => r.stack),
  configs: (params?: ListParams) =>
    fetchJSON<PagedResponse<Config>>(buildListURL("/configs", params)),
  config: (id: string, signal?: AbortSignal) => fetchJSON<ConfigDetail>(`/configs/${id}`, signal),
  secrets: (params?: ListParams) =>
    fetchJSON<PagedResponse<Secret>>(buildListURL("/secrets", params)),
  secret: (id: string, signal?: AbortSignal) => fetchJSON<SecretDetail>(`/secrets/${id}`, signal),
  networks: (params?: ListParams) =>
    fetchJSON<PagedResponse<Network>>(buildListURL("/networks", params)),
  network: (id: string, signal?: AbortSignal) => fetchJSON<NetworkDetail>(`/networks/${id}`, signal),
  volumes: (params?: ListParams) =>
    fetchJSON<PagedResponse<Volume>>(buildListURL("/volumes", params)),
  volume: (name: string, signal?: AbortSignal) => fetchJSON<VolumeDetail>(`/volumes/${name}`, signal),
  task: (id: string) => fetchJSON<{ task: Task }>(`/tasks/${id}`).then((r) => r.task),
  taskLogs: (id: string, opts?: LogOpts) =>
    fetchJSON<LogResponse>(`/tasks/${id}/logs?${buildLogParams(opts)}`, opts?.signal),
  serviceTasks: (id: string) =>
    fetchJSON<CollectionResponse<Task>>(`/services/${id}/tasks`).then((r) => r.items),
  serviceLogs: (id: string, opts?: LogOpts) =>
    fetchJSON<LogResponse>(`/services/${id}/logs?${buildLogParams(opts)}`, opts?.signal),
  serviceLogsStreamURL: (id: string, opts?: { after?: string; stream?: string }) =>
    buildLogStreamURL(`/services/${id}/logs`, opts),
  taskLogsStreamURL: (id: string, opts?: { after?: string; stream?: string }) =>
    buildLogStreamURL(`/tasks/${id}/logs`, opts),
  history: (
    params?: { type?: string; resourceId?: string; limit?: number },
    signal?: AbortSignal,
  ) => {
    const qs = new URLSearchParams();
    if (params?.type) qs.set("type", params.type);
    if (params?.resourceId) qs.set("resourceId", params.resourceId);
    if (params?.limit) qs.set("limit", String(params.limit));
    const query = qs.toString();
    return fetchJSON<CollectionResponse<HistoryEntry>>(
      `/history${query ? `?${query}` : ""}`,
      signal,
    ).then((r) => r.items);
  },
  topologyNetworks: () => fetchJSON<NetworkTopology>("/topology/networks"),
  topologyPlacement: () => fetchJSON<PlacementTopology>("/topology/placement"),
  nodeTasks: (id: string) =>
    fetchJSON<CollectionResponse<Task>>(`/nodes/${id}/tasks`).then((r) => r.items),
  metricsQuery: (query: string, time?: string) => {
    const params = new URLSearchParams({ query });
    if (time) params.set("time", time);
    return fetchJSON<PrometheusResponse>(`/-/metrics/query?${params}`);
  },
  metricsQueryRange: (query: string, start: string, end: string, step: string) => {
    const params = new URLSearchParams({ query, start, end, step });
    return fetchJSON<PrometheusResponse>(`/-/metrics/query_range?${params}`);
  },
  metricsStreamURL: (query: string, step: number, range: number): string => {
    const params = new URLSearchParams({ query, step: String(step), range: String(range) });
    return `/-/metrics/query_range?${params}`;
  },
  diskUsage: () =>
    fetchJSON<CollectionResponse<DiskUsageSummary>>("/disk-usage").then((r) => r.items),
  search: (q: string, limit?: number, signal?: AbortSignal) =>
    fetchJSON<SearchResponse>(
      `/search?q=${encodeURIComponent(q)}${limit !== undefined ? `&limit=${limit}` : ""}`,
      signal,
    ),
};
