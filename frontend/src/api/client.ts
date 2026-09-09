import * as schema from "./schemas";
import type {
  ClusterCapacity,
  CollectionResponse,
  Config,
  ConfigDetail,
  ContainerConfig,
  DiskUsageSummary,
  Healthcheck,
  HistoryEntry,
  Identity,
  JGFDocument,
  LicensesResponse,
  LogDriver,
  MonitoringStatus,
  Network,
  NetworkDetail,
  Node,
  PatchOp,
  Placement,
  Plugin,
  PluginPrivilege,
  PortConfig,
  PrometheusResponse,
  RecommendationsResponse,
  SearchResponse,
  Secret,
  SecretDetail,
  ServiceConfigRef,
  ServiceDetail,
  ServiceListItem,
  ServiceMount,
  ServiceNetworkRef,
  ServiceSecretRef,
  Stack,
  StackDetail,
  StackSummary,
  SwarmInfo,
  Task,
  UpdateConfig,
  Volume,
  VolumeDetail,
} from "./types";
import { apiPath } from "@/lib/basePath";
import type { z } from "zod";

const headers = { Accept: "application/json" };

/**
 * Structured API error carrying RFC 9457 problem detail fields.
 * The `code` property extracts the error code from the type URI
 * (e.g. "/api/errors/NOD001" → "NOD001").
 */
export class ApiError extends Error {
  readonly type: string;
  readonly title: string;
  readonly status: number;
  readonly detail: string;
  readonly code: string | null;

  constructor(type: string, title: string, status: number, detail: string) {
    super(detail || title);
    this.type = type;
    this.title = title;
    this.status = status;
    this.detail = detail;

    const match = type.match(/\/api\/errors\/([A-Z]{3}\d{3})$/);
    this.code = match?.[1] ?? null;
  }
}

export interface FetchResult<T> {
  data: T;
  allowedMethods: Set<string>;
}

function parseAllowHeader(response: Response): Set<string> {
  const header = response.headers.get("Allow");
  if (!header) {
    return new Set();
  }

  return new Set(header.split(",").map((method) => method.trim().toUpperCase()));
}

/** Sends the browser to the login page, remembering where it was. */
export function redirectToLogin(): void {
  const redirect = encodeURIComponent(window.location.pathname + window.location.search);
  window.location.href = apiPath(`/auth/login?redirect=${redirect}`);
}

function redirectToLoginAndStop(): never {
  redirectToLogin();

  // Throw to prevent callers from continuing while the browser navigates away.
  throw new Error("redirecting to login");
}

/**
 * Reads an RFC 9457 problem body into an ApiError, falling back to the status
 * text when the response carries no problem document.
 */
export async function problemFromResponse(response: Response): Promise<ApiError> {
  let type = "about:blank";
  let title = response.statusText;
  let detail = "";

  try {
    const body = await response.json();
    if (body?.type) {
      type = body.type;
    }
    if (body?.title) {
      title = body.title;
    }
    if (body?.detail) {
      detail = body.detail;
    }
  } catch {
    // response wasn't JSON
  }

  return new ApiError(type, title, response.status, detail);
}

async function throwResponseError(response: Response): Promise<never> {
  throw await problemFromResponse(response);
}

/** Default request timeout in milliseconds. */
const defaultTimeoutMilliseconds = 30_000;

/**
 * Combines two AbortSignals so the fetch aborts if either fires.
 * Returns the timeout signal directly if no caller signal is provided.
 */
function composeSignals(caller: AbortSignal | undefined, timeout: AbortSignal): AbortSignal {
  if (!caller) {
    return timeout;
  }

  return AbortSignal.any([caller, timeout]);
}

/**
 * Issues a read request and hands back the response once it is known good.
 * The 401-Bearer redirect and the timeout live here so every reader gets them
 * on the same terms; callers differ only in the Accept they ask for and how
 * they decode the body.
 */
async function request(
  path: string,
  requestHeaders: HeadersInit | undefined,
  signal: AbortSignal | undefined,
): Promise<Response> {
  const response = await fetch(apiPath(path), {
    ...(requestHeaders ? { headers: requestHeaders } : {}),
    signal: composeSignals(signal, AbortSignal.timeout(defaultTimeoutMilliseconds)),
  });

  if (!response.ok) {
    if (response.status === 401 && response.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLoginAndStop();
    }

    await throwResponseError(response);
  }

  return response;
}

/**
 * Endpoints already reported this session, so a page polling a drifted endpoint
 * every couple of seconds says it once rather than filling the console.
 */
const reportedDrift = new Set<string>();

/** Strips the query string, so `/services/abc?x=1` groups with `/services/abc`. */
function endpointOf(path: string): string {
  return path.split("?")[0] ?? path;
}

/**
 * Reports a response that is not the shape the dashboard was written against.
 *
 * Deliberately does not throw. The dashboard renders a server it disagrees with
 * exactly as it did before — a missing field was already going to show as a
 * blank cell — and this makes the reason visible instead of leaving it to be
 * guessed at from the symptom.
 */
function reportSchemaDrift(path: string, error: z.ZodError): void {
  const endpoint = endpointOf(path);

  if (reportedDrift.has(endpoint)) {
    return;
  }

  reportedDrift.add(endpoint);

  const issues = error.issues
    .slice(0, 5)
    .map(({ message, path: at }) => `${at.join(".") || "(root)"}: ${message}`);

  // eslint-disable-next-line no-console
  console.error(
    `[api] ${endpoint} is not the shape this dashboard expects. Rendering it anyway.`,
    issues,
  );
}

/** Test seam: schema reports are once-per-endpoint for the life of the page. */
export function resetSchemaDriftReports(): void {
  reportedDrift.clear();
}

async function fetchJSON<T>(
  path: string,
  signal?: AbortSignal,
  responseSchema?: z.ZodType,
): Promise<FetchResult<T>> {
  const response = await request(path, headers, signal);

  const allowedMethods = parseAllowHeader(response);
  const data = await response.json();

  if (responseSchema) {
    const result = responseSchema.safeParse(data);

    if (!result.success) {
      reportSchemaDrift(path, result.error);
    }
  }

  return { data, allowedMethods };
}

async function fetchJGF<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await request(path, { Accept: "application/vnd.jgf+json" }, signal);

  return response.json();
}

async function fetchText(path: string, signal?: AbortSignal): Promise<string> {
  const response = await request(path, undefined, signal);

  return response.text();
}

async function mutationFetch<T>(
  path: string,
  method: string,
  body?: unknown | undefined,
  contentType?: string | undefined,
): Promise<T> {
  const h: Record<string, string> = { Accept: "application/json" };
  if (contentType) {
    h["Content-Type"] = contentType;
  }
  const init: RequestInit = {
    method,
    headers: h,
    signal: AbortSignal.timeout(defaultTimeoutMilliseconds),
  };

  if (body !== undefined) {
    init.body = JSON.stringify(body);
  }

  const response = await fetch(apiPath(path), init);

  if (!response.ok) {
    if (response.status === 401 && response.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLoginAndStop();
    }

    await throwResponseError(response);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json();
}

export function get<T>(path: string, signal?: AbortSignal): Promise<FetchResult<T>> {
  return fetchJSON(path, signal);
}

export function put<T>(path: string, body: unknown): Promise<T> {
  return mutationFetch(path, "PUT", body, "application/json");
}

export function post<T>(path: string): Promise<T> {
  return mutationFetch(path, "POST");
}

export function patch<T>(path: string, body: unknown, contentType: string): Promise<T> {
  return mutationFetch(path, "PATCH", body, contentType);
}

export function del(path: string): Promise<void> {
  return mutationFetch(path, "DELETE");
}

export const emptyMethods: Set<string> = new Set();

export function setsEqual(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) {
    return false;
  }

  for (const item of a) {
    if (!b.has(item)) {
      return false;
    }
  }

  return true;
}

export async function headAllowedMethods(path: string): Promise<Set<string>> {
  const response = await fetch(apiPath(path), {
    method: "HEAD",
    headers,
    signal: AbortSignal.timeout(defaultTimeoutMilliseconds),
  });

  return parseAllowHeader(response);
}

export interface LogLine {
  timestamp: string;
  message: string;
  stream: "stdout" | "stderr";
  attrs?: Record<string, string> | undefined;
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
  localNodeID?: string | undefined;
}

export interface ClusterMetrics {
  cpu: { used: number; total: number; percent: number };
  memory: { used: number; total: number; percent: number };
  disk: { used: number; total: number; percent: number };
}

export interface LogOptions {
  limit?: number | undefined;
  after?: string | undefined;
  before?: string | undefined;
  stream?: string | undefined;
  signal?: AbortSignal | undefined;
}

function buildLogParams(options?: LogOptions): URLSearchParams {
  const params = new URLSearchParams({ limit: String(options?.limit || 500) });
  if (options?.after) {
    params.set("after", options.after);
  }

  if (options?.before) {
    params.set("before", options.before);
  }

  if (options?.stream) {
    params.set("stream", options.stream);
  }

  return params;
}

function buildLogStreamURL(
  path: string,
  options?: { after?: string | undefined; stream?: string | undefined },
): string {
  const params = new URLSearchParams();

  if (options?.after) {
    params.set("after", options.after);
  }

  if (options?.stream) {
    params.set("stream", options.stream);
  }

  const queryParams = params.toString();

  return apiPath(`${path}${queryParams ? `?${queryParams}` : ""}`);
}

export interface ListParams {
  offset?: number | undefined;
  sort?: string | undefined;
  dir?: "asc" | "desc" | undefined;
  search?: string | undefined;
  filter?: string | undefined;
}

/** Items per Range request page. Backend default is also 50; the max is 200. */
export const pageSize = 50;

function buildListQueryString(params?: ListParams): string {
  const queryParams = new URLSearchParams();

  if (params?.sort) {
    queryParams.set("sort", params.sort);
  }

  if (params?.dir) {
    queryParams.set("dir", params.dir);
  }

  if (params?.search) {
    queryParams.set("search", params.search);
  }

  if (params?.filter) {
    queryParams.set("filter", params.filter);
  }

  const query = queryParams.toString();

  return query ? `?${query}` : "";
}

async function fetchRange<T>(
  path: string,
  params?: ListParams | undefined,
  signal?: AbortSignal | undefined,
  itemSchema?: z.ZodType,
): Promise<FetchResult<CollectionResponse<T>>> {
  const offset = params?.offset ?? 0;
  const end = offset + pageSize - 1;
  const url = `${path}${buildListQueryString(params)}`;

  const response = await fetch(apiPath(url), {
    headers: {
      Accept: "application/json",
      Range: `items ${offset}-${end}`,
    },
    signal: composeSignals(signal, AbortSignal.timeout(defaultTimeoutMilliseconds)),
  });

  if (!response.ok) {
    if (response.status === 401 && response.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLoginAndStop();
    }

    // 416 says the requested offset is past the end of the collection, which
    // happens whenever the client's idea of the total is stale — items were
    // removed between pages. That is a correct answer to a stale question, not
    // a failure: report an empty page carrying the authoritative total from
    // Content-Range so the caller stops paging instead of showing an error.
    if (response.status === 416) {
      const total = totalFromContentRange(response.headers.get("Content-Range"));

      if (total !== null) {
        return {
          data: { items: [], total, limit: pageSize, offset },
          allowedMethods: parseAllowHeader(response),
        };
      }
    }

    await throwResponseError(response);
  }

  const allowedMethods = parseAllowHeader(response);
  const data = await response.json();

  if (itemSchema) {
    const result = schema.collectionOf(itemSchema).safeParse(data);

    if (!result.success) {
      reportSchemaDrift(path, result.error);
    }
  }

  return { data, allowedMethods };
}

/** Reads the total out of an unsatisfied range response's Content-Range header. */
function totalFromContentRange(header: string | null): number | null {
  const match = /^items \*\/(\d+)$/.exec(header?.trim() ?? "");

  return match ? Number(match[1]) : null;
}

export const api = {
  whoami: () =>
    fetchJSON<Identity>("/profile", undefined, schema.identitySchema).then(({ data }) => data),
  cluster: () =>
    fetchJSON<ClusterSnapshot>("/cluster", undefined, schema.clusterSchema).then(
      ({ data }) => data,
    ),
  resync: () => post<{ status: string; durationMs?: number }>("/-/resync"),
  swarm: () => fetchJSON<SwarmInfo>("/swarm", undefined, schema.swarmSchema),
  unlockKey: () =>
    fetchJSON<{ unlockKey: string }>("/swarm/unlock-key", undefined, schema.unlockKeySchema).then(
      ({ data }) => data,
    ),
  patchSwarmOrchestration: (data: Record<string, unknown>) =>
    patch("/swarm/orchestration", data, "application/merge-patch+json"),
  patchSwarmRaft: (data: Record<string, unknown>) =>
    patch("/swarm/raft", data, "application/merge-patch+json"),
  patchSwarmDispatcher: (data: Record<string, unknown>) =>
    patch("/swarm/dispatcher", data, "application/merge-patch+json"),
  patchSwarmCAConfig: (data: Record<string, unknown>) =>
    patch("/swarm/ca", data, "application/merge-patch+json"),
  patchSwarmEncryption: (data: Record<string, unknown>) =>
    patch("/swarm/encryption", data, "application/merge-patch+json"),
  rotateToken: (target: "worker" | "manager") =>
    mutationFetch<void>("/swarm/rotate-token", "POST", { target }, "application/json"),
  rotateUnlockKey: () => mutationFetch<void>("/swarm/rotate-unlock-key", "POST"),
  unlockSwarm: (unlockKey: string) =>
    mutationFetch<void>("/swarm/unlock", "POST", { unlockKey }, "application/json"),
  forceRotateCA: () => mutationFetch<void>("/swarm/force-rotate-ca", "POST"),
  plugins: () =>
    fetchJSON<CollectionResponse<Plugin>>(
      "/plugins",
      undefined,
      schema.collectionOf(schema.pluginSchema),
    ).then(({ data, allowedMethods }) => ({
      data: data.items,
      allowedMethods,
    })),
  plugin: (name: string, signal?: AbortSignal) =>
    fetchJSON<{ plugin: Plugin }>(
      `/plugins/${encodeURIComponent(name)}`,
      signal,
      schema.pluginDetailSchema,
    ).then(({ data, allowedMethods }) => ({ data: data.plugin, allowedMethods })),
  pluginPrivileges: (remote: string) =>
    mutationFetch<{ privileges: PluginPrivilege[] }>(
      "/plugins/privileges",
      "POST",
      { remote },
      "application/json",
    ).then(({ privileges }) => privileges),
  installPlugin: (remote: string) =>
    mutationFetch<{ plugin: Plugin }>("/plugins", "POST", { remote }, "application/json"),
  enablePlugin: (name: string) => post<void>(`/plugins/${encodeURIComponent(name)}/enable`),
  disablePlugin: (name: string) => post<void>(`/plugins/${encodeURIComponent(name)}/disable`),
  removePlugin: (name: string, force?: boolean) =>
    del(`/plugins/${encodeURIComponent(name)}${force ? "?force=true" : ""}`),
  upgradePlugin: (name: string, remote: string) =>
    mutationFetch<void>(
      `/plugins/${encodeURIComponent(name)}/upgrade`,
      "POST",
      { remote },
      "application/json",
    ),
  configurePlugin: (name: string, settings: { args?: string[]; env?: string[] }) =>
    patch<void>(`/plugins/${encodeURIComponent(name)}/settings`, settings, "application/json"),
  clusterMetrics: () =>
    fetchJSON<ClusterMetrics>("/cluster/metrics", undefined, schema.clusterMetricsSchema).then(
      ({ data }) => data,
    ),
  monitoringStatus: () =>
    fetchJSON<MonitoringStatus>("/metrics/status", undefined, schema.monitoringStatusSchema).then(
      ({ data }) => data,
    ),
  nodes: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Node>("/nodes", params, signal, schema.nodeSchema),
  node: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ node: Node }>(`/nodes/${id}`, signal, schema.nodeDetailSchema).then(
      ({ data, allowedMethods }) => ({
        data: data.node,
        allowedMethods,
      }),
    ),
  services: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<ServiceListItem>("/services", params, signal, schema.serviceListSchema),
  recommendations: () =>
    fetchJSON<RecommendationsResponse>(
      "/recommendations",
      undefined,
      schema.recommendationsSchema,
    ).then(({ data }) => data),
  service: (id: string, signal?: AbortSignal) =>
    fetchJSON<ServiceDetail>(`/services/${id}`, signal, schema.serviceDetailSchema),
  tasks: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Task>("/tasks", params, signal, schema.taskSchema),
  stacks: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Stack>("/stacks", params, signal, schema.stackListSchema),
  stacksSummary: () =>
    fetchJSON<CollectionResponse<StackSummary>>(
      "/stacks/summary",
      undefined,
      schema.collectionOf(schema.stackSummarySchema),
    ).then(({ data }) => data.items),
  stack: (name: string, signal?: AbortSignal) =>
    fetchJSON<{ stack: StackDetail }>(
      `/stacks/${name}`,
      signal,
      schema.stackDetailResponseSchema,
    ).then(({ data, allowedMethods }) => ({
      data: data.stack,
      allowedMethods,
    })),
  configs: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Config>("/configs", params, signal, schema.configSchema),
  config: (id: string, signal?: AbortSignal) =>
    fetchJSON<ConfigDetail>(`/configs/${id}`, signal, schema.configDetailSchema),
  secrets: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Secret>("/secrets", params, signal, schema.secretSchema),
  secret: (id: string, signal?: AbortSignal) =>
    fetchJSON<SecretDetail>(`/secrets/${id}`, signal, schema.secretDetailSchema),
  networks: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Network>("/networks", params, signal, schema.networkSchema),
  network: (id: string, signal?: AbortSignal) =>
    fetchJSON<NetworkDetail>(`/networks/${id}`, signal, schema.networkDetailSchema),
  volumes: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Volume>("/volumes", params, signal, schema.volumeSchema),
  volume: (name: string, signal?: AbortSignal) =>
    fetchJSON<VolumeDetail>(`/volumes/${name}`, signal, schema.volumeDetailSchema),
  task: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ task: Task }>(`/tasks/${id}`, signal, schema.taskDetailSchema).then(
      ({ data, allowedMethods }) => ({
        data: data.task,
        allowedMethods,
      }),
    ),
  taskLogs: (id: string, options?: LogOptions) =>
    fetchJSON<LogResponse>(
      `/tasks/${id}/logs?${buildLogParams(options)}`,
      options?.signal,
      schema.logResponseSchema,
    ).then(({ data }) => data),
  serviceTasks: (id: string, signal?: AbortSignal) =>
    fetchJSON<CollectionResponse<Task>>(
      `/services/${id}/tasks`,
      signal,
      schema.collectionOf(schema.taskSchema),
    ).then(({ data }) => data.items),
  serviceLogs: (id: string, options?: LogOptions) =>
    fetchJSON<LogResponse>(
      `/services/${id}/logs?${buildLogParams(options)}`,
      options?.signal,
      schema.logResponseSchema,
    ).then(({ data }) => data),
  serviceLogsStreamURL: (
    id: string,
    options?: { after?: string | undefined; stream?: string | undefined },
  ) => buildLogStreamURL(`/services/${id}/logs`, options),
  taskLogsStreamURL: (
    id: string,
    options?: { after?: string | undefined; stream?: string | undefined },
  ) => buildLogStreamURL(`/tasks/${id}/logs`, options),
  history: async (
    params?: { type?: string; resourceId?: string; limit?: number } | undefined,
    signal?: AbortSignal | undefined,
  ) => {
    const queryParams = new URLSearchParams();

    if (params?.type) {
      queryParams.set("type", params.type);
    }

    if (params?.resourceId) {
      queryParams.set("resourceId", params.resourceId);
    }

    if (params?.limit) {
      queryParams.set("limit", String(params.limit));
    }

    const query = queryParams.toString();
    const { data } = await fetchJSON<CollectionResponse<HistoryEntry>>(
      `/history${query ? `?${query}` : ""}`,
      signal,
      schema.collectionOf(schema.historyEntrySchema),
    );

    return data.items;
  },
  topology: () => fetchJGF<JGFDocument>("/topology"),
  nodeTasks: (id: string, signal?: AbortSignal) =>
    fetchJSON<CollectionResponse<Task>>(
      `/nodes/${id}/tasks`,
      signal,
      schema.collectionOf(schema.taskSchema),
    ).then(({ data }) => data.items),
  metricsQuery: async (query: string, time?: string) => {
    const params = new URLSearchParams({ query });

    if (time) {
      params.set("time", time);
    }

    const { data } = await fetchJSON<PrometheusResponse>(
      `/metrics?${params}`,
      undefined,
      schema.prometheusSchema,
    );

    return data;
  },
  metricsQueryRange: async (query: string, start: string, end: string, step: string) => {
    const params = new URLSearchParams({ query, start, end, step });
    const { data } = await fetchJSON<PrometheusResponse>(
      `/metrics?${params}`,
      undefined,
      schema.prometheusSchema,
    );

    return data;
  },
  metricsStreamURL: (query: string, step: number, range: number): string => {
    const params = new URLSearchParams({ query, step: String(step), range: String(range) });
    return apiPath(`/metrics?${params}`);
  },
  diskUsage: () =>
    fetchJSON<CollectionResponse<DiskUsageSummary>>(
      "/disk-usage",
      undefined,
      schema.collectionOf(schema.diskUsageSchema),
    ).then(({ data }) => data.items),
  clusterCapacity: () =>
    fetchJSON<ClusterCapacity>("/cluster/capacity", undefined, schema.clusterCapacitySchema).then(
      ({ data }) => data,
    ),
  dockerLatestVersion: () =>
    fetchJSON<{ version: string; url: string }>(
      "/-/docker-latest-version",
      undefined,
      schema.dockerVersionSchema,
    ).then(({ data }) => data),
  metricsLabels: async (match?: string) => {
    const params = new URLSearchParams();

    if (match) {
      params.set("match[]", match);
    }

    const { data } = await fetchJSON<{ data: string[] }>(
      `/metrics/labels?${params}`,
      undefined,
      schema.metricsLabelsSchema,
    );

    return data.data;
  },
  metricsLabelValues: (name: string) =>
    fetchJSON<{ data: string[] }>(
      `/metrics/labels/${encodeURIComponent(name)}`,
      undefined,
      schema.metricsLabelsSchema,
    ).then(({ data }) => data.data),
  search: (q: string, limit?: number, signal?: AbortSignal) =>
    fetchJSON<SearchResponse>(
      `/search?q=${encodeURIComponent(q)}${limit !== undefined ? `&limit=${limit}` : ""}`,
      signal,
      schema.searchSchema,
    ).then(({ data }) => data),
  scaleService: (id: string, replicas: number) =>
    put<ServiceDetail>(`/services/${id}/scale`, { replicas }),
  updateServiceMode: (id: string, mode: "replicated" | "global", replicas?: number) =>
    put<ServiceDetail>(`/services/${id}/mode`, { mode, replicas }),
  updateServiceEndpointMode: (id: string, mode: "vip" | "dnsrr") =>
    put<ServiceDetail>(`/services/${id}/endpoint-mode`, { mode }),
  updateServiceImage: (id: string, image: string) =>
    put<ServiceDetail>(`/services/${id}/image`, { image }),
  rollbackService: (id: string) => post<ServiceDetail>(`/services/${id}/rollback`),
  restartService: (id: string) => post<ServiceDetail>(`/services/${id}/restart`),
  updateNodeAvailability: (id: string, availability: "active" | "drain" | "pause") =>
    put<{ node: Node }>(`/nodes/${id}/availability`, { availability }),
  removeTask: (id: string) => del(`/tasks/${id}`),
  removeService: (id: string) => del(`/services/${id}`),
  updateNodeRole: (id: string, role: "worker" | "manager") =>
    put<{ node: Node }>(`/nodes/${id}/role`, { role }),
  removeNode: (id: string, force?: boolean) =>
    del(force ? `/nodes/${id}?force=true` : `/nodes/${id}`),
  removeStack: (name: string) =>
    mutationFetch<{
      removed: { services: number; networks: number; configs: number; secrets: number };
      errors?: { type: string; id: string; error: string }[] | undefined;
    }>(`/stacks/${name}`, "DELETE"),
  removeConfig: (id: string) => del(`/configs/${id}`),
  removeSecret: (id: string) => del(`/secrets/${id}`),
  createConfig: (name: string, data: string) =>
    mutationFetch<ConfigDetail>("/configs", "POST", { name, data }, "application/json"),
  createSecret: (name: string, data: string) =>
    mutationFetch<SecretDetail>("/secrets", "POST", { name, data }, "application/json"),
  patchConfigLabels: (id: string, ops: PatchOp[]) =>
    patch<{ labels: Record<string, string> }>(
      `/configs/${id}/labels`,
      ops,
      "application/json-patch+json",
    ).then(({ labels }) => labels),
  patchSecretLabels: (id: string, ops: PatchOp[]) =>
    patch<{ labels: Record<string, string> }>(
      `/secrets/${id}/labels`,
      ops,
      "application/json-patch+json",
    ).then(({ labels }) => labels),
  removeNetwork: (id: string) => del(`/networks/${id}`),
  removeVolume: (name: string, force?: boolean) =>
    del(force ? `/volumes/${name}?force=true` : `/volumes/${name}`),

  // Tier 2: sub-resource GETs
  serviceEnv: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ env: Record<string, string> }>(
      `/services/${id}/env`,
      signal,
      schema.envSchema,
    ).then(({ data }) => data.env),
  nodeLabels: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ labels: Record<string, string> }>(
      `/nodes/${id}/labels`,
      signal,
      schema.labelsSchema,
    ).then(({ data }) => data.labels),
  nodeRole: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ role: "worker" | "manager"; isLeader: boolean; managerCount: number }>(
      `/nodes/${id}/role`,
      signal,
      schema.nodeRoleSchema,
    ).then(({ data }) => data),
  serviceLabels: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ labels: Record<string, string> }>(
      `/services/${id}/labels`,
      signal,
      schema.labelsSchema,
    ).then(({ data }) => data.labels),
  serviceResources: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ resources: Record<string, unknown> }>(
      `/services/${id}/resources`,
      signal,
      schema.resourcesSchema,
    ).then(({ data }) => data.resources),
  serviceHealthcheck: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ healthcheck: Healthcheck | null }>(
      `/services/${id}/healthcheck`,
      signal,
      schema.healthcheckSchema,
    ).then(({ data }) => data.healthcheck),
  serviceConfigs: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ configs: ServiceConfigRef[] }>(
      `/services/${id}/configs`,
      signal,
      schema.serviceConfigsSchema,
    ).then(({ data }) => data.configs),
  serviceSecrets: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ secrets: ServiceSecretRef[] }>(
      `/services/${id}/secrets`,
      signal,
      schema.serviceSecretsSchema,
    ).then(({ data }) => data.secrets),
  serviceNetworks: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ networks: ServiceNetworkRef[] }>(
      `/services/${id}/networks`,
      signal,
      schema.serviceNetworksSchema,
    ).then(({ data }) => data.networks),
  serviceMounts: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ mounts: ServiceMount[] }>(
      `/services/${id}/mounts`,
      signal,
      schema.mountsSchema,
    ).then(({ data }) => data.mounts ?? []),

  // Tier 2: sub-resource PATCHes
  patchServiceEnv: (id: string, ops: PatchOp[]) =>
    patch<{ env: Record<string, string> }>(
      `/services/${id}/env`,
      ops,
      "application/json-patch+json",
    ).then(({ env }) => env),
  patchNodeLabels: (id: string, ops: PatchOp[]) =>
    patch<{ labels: Record<string, string> }>(
      `/nodes/${id}/labels`,
      ops,
      "application/json-patch+json",
    ).then(({ labels }) => labels),
  patchServiceLabels: (id: string, ops: PatchOp[]) =>
    patch<{ labels: Record<string, string> }>(
      `/services/${id}/labels`,
      ops,
      "application/json-patch+json",
    ).then(({ labels }) => labels),
  patchServiceResources: (id: string, partial: unknown) =>
    patch<{ resources: Record<string, unknown> }>(
      `/services/${id}/resources`,
      partial,
      "application/merge-patch+json",
    ).then(({ resources }) => resources),
  putServiceHealthcheck: (id: string, healthcheck: Healthcheck) =>
    put<{ healthcheck: Healthcheck }>(`/services/${id}/healthcheck`, healthcheck),

  servicePorts: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ ports: PortConfig[] }>(`/services/${id}/ports`, signal, schema.portsSchema).then(
      ({ data }) => data.ports,
    ),

  servicePlacement: (id: string) =>
    fetchJSON<{ placement: Placement }>(
      `/services/${id}/placement`,
      undefined,
      schema.placementSchema,
    ).then(({ data }) => data.placement),

  putServicePlacement: (id: string, placement: Placement) =>
    put<{ placement: Placement }>(`/services/${id}/placement`, placement),

  patchServicePorts: (id: string, ports: PortConfig[]) =>
    patch<{ ports: PortConfig[] }>(
      `/services/${id}/ports`,
      { ports },
      "application/merge-patch+json",
    ),
  patchServiceConfigs: (id: string, configs: ServiceConfigRef[]) =>
    patch<{ configs: ServiceConfigRef[] }>(
      `/services/${id}/configs`,
      { configs },
      "application/merge-patch+json",
    ),
  patchServiceSecrets: (id: string, secrets: ServiceSecretRef[]) =>
    patch<{ secrets: ServiceSecretRef[] }>(
      `/services/${id}/secrets`,
      { secrets },
      "application/merge-patch+json",
    ),
  patchServiceNetworks: (id: string, networks: ServiceNetworkRef[]) =>
    patch<{ networks: ServiceNetworkRef[] }>(
      `/services/${id}/networks`,
      { networks },
      "application/merge-patch+json",
    ),
  patchServiceMounts: (id: string, mounts: ServiceMount[]) =>
    patch<{ mounts: ServiceMount[] }>(
      `/services/${id}/mounts`,
      { mounts },
      "application/merge-patch+json",
    ),

  patchServiceUpdatePolicy: (id: string, partial: Record<string, unknown>) =>
    patch<{ updatePolicy: UpdateConfig }>(
      `/services/${id}/update-policy`,
      partial,
      "application/merge-patch+json",
    ),

  patchServiceRollbackPolicy: (id: string, partial: Record<string, unknown>) =>
    patch<{ rollbackPolicy: UpdateConfig }>(
      `/services/${id}/rollback-policy`,
      partial,
      "application/merge-patch+json",
    ),

  patchServiceLogDriver: (id: string, partial: Record<string, unknown>) =>
    patch<{ logDriver: LogDriver }>(
      `/services/${id}/log-driver`,
      partial,
      "application/merge-patch+json",
    ),

  serviceContainerConfig: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ containerConfig: ContainerConfig }>(
      `/services/${id}/container-config`,
      signal,
      schema.containerConfigSchema,
    ),

  patchServiceContainerConfig: (id: string, partial: Record<string, unknown>) =>
    patch<{ containerConfig: ContainerConfig }>(
      `/services/${id}/container-config`,
      partial,
      "application/merge-patch+json",
    ).then(({ containerConfig }) => containerConfig),

  health: (signal?: AbortSignal) =>
    fetchJSON<HealthInfo>(`/-/health`, signal, schema.healthSchema).then(({ data }) => data),

  licenses: (signal?: AbortSignal) =>
    fetchJSON<LicensesResponse>(`/-/licenses`, signal, schema.licensesSchema).then(
      ({ data }) => data,
    ),

  licenseText: (id: string, signal?: AbortSignal) =>
    fetchText(`/-/licenses/texts/${encodeURIComponent(id)}`, signal),
};

export interface HealthInfo {
  status: string;
  version: string;
  commit: string;
  buildDate: string;
  operationsLevel: number;
}
