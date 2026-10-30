import * as schema from "./schemas";
import type {
  PublishedPort,
  ClusterCapacity,
  CollectionResponse,
  Config,
  ConfigDetail,
  ConfigListItem,
  ContainerConfig,
  DiskUsageSummary,
  DrainImpact,
  Healthcheck,
  HistoryEntry,
  ImageDockerfile,
  ImageProvenance,
  ImageSBOM,
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
  SecretListItem,
  SecretDetail,
  SecretEntry,
  ServiceConfigRef,
  ServiceDetail,
  ServiceListItem,
  ServiceMount,
  ServiceNetworkRef,
  ServiceSecretRef,
  Stack,
  StackResponse,
  StackSummary,
  SwarmInfo,
  Task,
  UpdateConfig,
  Volume,
  VolumeDetail,
} from "./types";
import { apiPath, stripBasePath } from "@/lib/basePath";
import { linkTarget, linkTargets } from "@/lib/linkHeader";
import { z } from "zod";

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

  /**
   * The sub-resources the caller may write, by name (`scale`, `labels`), read
   * from the response's `rel="edit"` links. Its own methods are in `Allow`.
   */
  editable?: Set<string> | undefined;

  /**
   * The resource's canonical path, read from the response's JSON-LD `@id`.
   * Absent where the response carries none, which the demo handlers and the
   * endpoints outside the resource model both do.
   */
  canonicalPath?: string | undefined;
}

/**
 * Names the sub-resources a response links with `rel="edit"`, relative to the
 * path it was served from after any redirect, so a page reached by a name
 * matches the links its canonical path produced.
 */
function editableOf(response: Response): Set<string> {
  const base = response.url || window.location.href;
  const self = new URL(base).pathname;
  const names = new Set<string>();

  for (const target of linkTargets(response.headers.get("Link"), "edit")) {
    const path = new URL(target, base).pathname;
    const name = path.slice(self.length + 1);

    if (path.startsWith(`${self}/`) && name !== "" && !name.includes("/")) {
      names.add(name);
    }
  }

  return names;
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

/**
 * Thrown while the browser navigates to the login page. It is not a failed
 * request — retrying it would repeat both the fetch and the navigation — which
 * is why `queryClient` matches on the type rather than on a message.
 */
export class LoginRedirectError extends Error {
  constructor() {
    super("redirecting to login");
    this.name = "LoginRedirectError";
  }
}

function redirectToLoginAndStop(): never {
  redirectToLogin();

  // Throw to prevent callers from continuing while the browser navigates away.
  throw new LoginRedirectError();
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
 * Outlasts the server's two bounds on a resync, two minutes each: waiting for
 * the event loop, then the sweep. Shorter, the 504 would never arrive.
 */
const resyncTimeoutMilliseconds = 300_000;

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
 * Issues a read request and hands back the response once it is known good. The
 * 401-Bearer redirect and the timeout live here so every reader gets them.
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
 * Deliberately does not throw: it makes the reason visible without changing how
 * the dashboard renders a server it disagrees with.
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

  // The endpoint is a substitution rather than part of the format string:
  // console.error treats its first argument as one, and the path can carry a
  // resource id out of the URL bar.
  // eslint-disable-next-line no-console
  console.error(
    "[api] %s is not the shape this dashboard expects. Rendering it anyway.",
    endpoint,
    issues,
  );
}

/** Warns once per endpoint when a response is not the shape it claims. */
function checkShape(endpoint: string, responseSchema: z.ZodType, data: unknown): void {
  const result = responseSchema.safeParse(data);

  if (!result.success) {
    reportSchemaDrift(endpoint, result.error);
  }
}

/** Test seam: schema reports are once-per-endpoint for the life of the page. */
export function resetSchemaDriftReports(): void {
  reportedDrift.clear();
}

async function fetchJSON<T>(
  path: string,
  signal: AbortSignal | undefined,
  responseSchema: z.ZodType,
): Promise<FetchResult<T>> {
  const response = await request(path, headers, signal);

  const allowedMethods = parseAllowHeader(response);
  const data = await response.json();

  checkShape(path, responseSchema, data);

  return {
    data,
    allowedMethods,
    editable: editableOf(response),
    canonicalPath: canonicalPathOf(data),
  };
}

/**
 * The resources whose image attestations can be read: a service's image is the
 * one its spec names, a task's the one it runs.
 */
export type AttestedKind = "service" | "task";

/**
 * The path of an image's SBOM or Dockerfile, with the suffix that picks a
 * download representation when one is given.
 */
export function attestationPath(
  kind: AttestedKind,
  id: string,
  resource: "sbom" | "dockerfile",
  platform?: string | undefined,
  suffix = "",
) {
  const query = platform ? `?platform=${encodeURIComponent(platform)}` : "";

  return `/${kind}s/${encodeURIComponent(id)}/${resource}${suffix}${query}`;
}

/**
 * A detail response names itself in `@id`, already spelled with the canonical
 * ID — so a page reached by name learns the ID without knowing where its type
 * keeps one.
 */
function canonicalPathOf(data: unknown): string | undefined {
  if (typeof data !== "object" || data === null) {
    return undefined;
  }

  const id = (data as Record<string, unknown>)["@id"];

  return typeof id === "string" ? stripBasePath(id) : undefined;
}

async function fetchJGF<T>(
  path: string,
  responseSchema: z.ZodType,
  signal?: AbortSignal,
): Promise<T> {
  const response = await request(path, { Accept: "application/vnd.jgf+json" }, signal);
  const data = await response.json();

  checkShape(path, responseSchema, data);

  return data;
}

async function fetchText(path: string, signal?: AbortSignal, accept?: string): Promise<string> {
  const response = await request(path, accept ? { Accept: accept } : undefined, signal);

  return response.text();
}

async function mutationFetch<T>(
  path: string,
  method: string,
  body?: unknown | undefined,
  contentType?: string | undefined,
  ifMatch?: string | undefined,
  timeoutMilliseconds = defaultTimeoutMilliseconds,
): Promise<T> {
  const h: Record<string, string> = { Accept: "application/json" };
  if (contentType) {
    h["Content-Type"] = contentType;
  }

  if (ifMatch) {
    h["If-Match"] = ifMatch;
  }
  const init: RequestInit = {
    method,
    headers: h,
    signal: AbortSignal.timeout(timeoutMilliseconds),
  };

  if (body !== undefined) {
    init.body = JSON.stringify(body);
  }

  const response = await fetch(apiPath(path), init);

  if (!response.ok) {
    if (response.status === 401 && response.headers.get("WWW-Authenticate")?.startsWith("Bearer")) {
      redirectToLoginAndStop();
    }

    if (response.status === 412) {
      const { type, title } = await problemFromResponse(response);

      throw new ApiError(type, title, 412, staleWriteMessage);
    }

    await throwResponseError(response);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json();
}

/**
 * The one deliberately unshaped read: `ServiceSubResource` renders whichever
 * sub-resource the URL names, so there is no single schema to hold it to.
 */
export function get<T>(path: string, signal?: AbortSignal): Promise<FetchResult<T>> {
  return fetchJSON(path, signal, z.unknown());
}

/** What a write refused with 412 tells the user. */
export const staleWriteMessage =
  "This changed since you loaded it. Reload to see the current version, then make your change again.";

/**
 * The ETag a GET of the path would carry now, to send back as `If-Match` on a
 * write. Undefined when it cannot be read; the write then goes unconditioned.
 */
export async function currentETag(path: string): Promise<string | undefined> {
  try {
    const response = await fetch(apiPath(path), {
      method: "HEAD",
      headers,
      signal: AbortSignal.timeout(defaultTimeoutMilliseconds),
    });

    return (response.ok && response.headers.get("ETag")) || undefined;
  } catch {
    return undefined;
  }
}

export function put<T>(path: string, body: unknown, ifMatch?: string | undefined): Promise<T> {
  return mutationFetch(path, "PUT", body, "application/json", ifMatch);
}

export function post<T>(path: string): Promise<T> {
  return mutationFetch(path, "POST");
}

export function patch<T>(
  path: string,
  body: unknown,
  contentType: string,
  ifMatch?: string | undefined,
): Promise<T> {
  return mutationFetch(path, "PATCH", body, contentType, ifMatch);
}

export function del(path: string): Promise<void> {
  return mutationFetch(path, "DELETE");
}

export const emptyMethods: Set<string> = new Set();

export interface Capabilities {
  /** The resource's own methods, from `Allow`. */
  methods: Set<string>;

  /** The sub-resources the caller may write, from `rel="edit"` links. */
  editable: Set<string>;
}

export async function headCapabilities(path: string): Promise<Capabilities> {
  const response = await fetch(apiPath(path), {
    method: "HEAD",
    headers,
    signal: AbortSignal.timeout(defaultTimeoutMilliseconds),
  });

  return { methods: parseAllowHeader(response), editable: editableOf(response) };
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
  quorum: ManagerQuorum;
  prometheusConfigured: boolean;
  localNodeID?: string | undefined;
}

export interface ManagerQuorum {
  managers: number;
  reachable: number;
  size: number;
  held: boolean;
  canLose: number;
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
  /**
   * Configs and secrets: `all` lists every version, not only each group's newest.
   */
  versions?: "newest" | "all" | undefined;
  /**
   * Configs and secrets: only the versions grouped with this ID, newest first.
   */
  versionsOf?: string | undefined;
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

  if (params?.versions) {
    queryParams.set("versions", params.versions);
  }

  if (params?.versionsOf) {
    queryParams.set("versionsOf", params.versionsOf);
  }

  const query = queryParams.toString();

  return query ? `?${query}` : "";
}

async function fetchRange<T>(
  path: string,
  params: ListParams | undefined,
  signal: AbortSignal | undefined,
  itemSchema: z.ZodType,
  size = pageSize,
): Promise<FetchResult<CollectionResponse<T>>> {
  const offset = params?.offset ?? 0;
  const end = offset + size - 1;
  const url = `${path}${buildListQueryString(params)}`;

  const response = await fetch(apiPath(url), {
    headers: {
      Accept: "application/json",
      Range: `items=${offset}-${end}`,
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
          data: { items: [], total, limit: size, offset },
          allowedMethods: parseAllowHeader(response),
        };
      }
    }

    await throwResponseError(response);
  }

  const allowedMethods = parseAllowHeader(response);
  const data = await response.json();

  checkShape(path, schema.collectionOf(itemSchema), data);

  return { data, allowedMethods };
}

/** The largest page the server serves; it clamps a longer range to this. */
const maximumPageSize = 200;

/**
 * Every item of a collection, for a picker that must offer all of them. Pages
 * by what arrived rather than by the requested size, so a clamp cannot skip any.
 */
async function fetchAll<T>(
  path: string,
  signal: AbortSignal | undefined,
  itemSchema: z.ZodType,
  params?: ListParams,
): Promise<T[]> {
  const items: T[] = [];

  for (;;) {
    // eslint-disable-next-line no-await-in-loop
    const { data } = await fetchRange<T>(
      path,
      { ...params, offset: items.length },
      signal,
      itemSchema,
      maximumPageSize,
    );
    items.push(...data.items);

    if (data.items.length === 0 || items.length >= data.total) {
      return items;
    }
  }
}

export interface HistoryPage {
  items: HistoryEntry[];

  /** The path of the next, older page, while the server says one exists. */
  next?: string | undefined;
}

/** The path of the first `/history` page for these filters. */
function historyPath(
  params?:
    | {
        type?: string;
        resourceId?: string;
        search?: string;
        kind?: string;
        since?: string;
        limit?: number;
      }
    | undefined,
): string {
  const queryParams = new URLSearchParams();

  if (params?.type) {
    queryParams.set("type", params.type);
  }

  if (params?.resourceId) {
    queryParams.set("resourceId", params.resourceId);
  }

  if (params?.search) {
    queryParams.set("search", params.search);
  }

  if (params?.kind) {
    queryParams.set("kind", params.kind);
  }

  if (params?.since) {
    queryParams.set("since", params.since);
  }

  if (params?.limit) {
    queryParams.set("limit", String(params.limit));
  }

  const query = queryParams.toString();

  return `/history${query ? `?${query}` : ""}`;
}

/**
 * The newest `limit` changes, and the gaps since the oldest of them, newest
 * first. One page holding both could fill up with gaps and show no change.
 */
async function fetchRecentActivity(
  params: { type?: string; resourceId?: string },
  limit: number,
  signal?: AbortSignal | undefined,
): Promise<HistoryEntry[]> {
  const { items: changes } = await fetchHistoryPage(
    historyPath({ ...params, kind: "change", limit }),
    signal,
  );
  const oldest = changes.at(-1);

  if (oldest === undefined) {
    return [];
  }

  const { items: gaps } = await fetchHistoryPage(
    historyPath({ kind: "gap", since: oldest.timestamp, limit: maximumPageSize }),
    signal,
  );

  return [...changes, ...gaps].sort((first, second) => second.id - first.id);
}

/**
 * Fetches one `/history` page. The `next` link pages by cursor, and it is
 * followed as the server wrote it rather than rebuilt from the last entry.
 */
async function fetchHistoryPage(
  path: string,
  signal?: AbortSignal | undefined,
): Promise<HistoryPage> {
  const response = await request(path, headers, signal);
  const data = (await response.json()) as CollectionResponse<HistoryEntry>;

  checkShape(path, schema.collectionOf(schema.historyEntrySchema), data);

  const target = linkTarget(response.headers.get("Link"), "next");

  if (target === undefined) {
    return { items: data.items };
  }

  const next = new URL(target, response.url || window.location.href);

  return { items: data.items, next: stripBasePath(next.pathname) + next.search };
}

/** Reads the total out of an unsatisfied range response's Content-Range header. */
function totalFromContentRange(header: string | null): number | null {
  const match = /^items \*\/(\d+)$/.exec(header?.trim() ?? "");

  return match ? Number(match[1]) : null;
}

export type ErrorDefinition = z.infer<typeof schema.errorDefinitionSchema>;

export const api = {
  errorDefinitions: () =>
    fetchJSON<{ items: ErrorDefinition[] }>(
      "/api/errors",
      undefined,
      schema.collectionOf(schema.errorDefinitionSchema),
    ).then(({ data }) => data.items),
  errorDefinition: (code: string) =>
    fetchJSON<ErrorDefinition>(
      `/api/errors/${encodeURIComponent(code)}`,
      undefined,
      schema.errorDefinitionSchema,
    ).then(({ data }) => data),
  whoami: () =>
    fetchJSON<Identity>("/profile", undefined, schema.identitySchema).then(({ data }) => data),
  cluster: () =>
    fetchJSON<ClusterSnapshot>("/cluster", undefined, schema.clusterSchema).then(
      ({ data }) => data,
    ),
  resync: () =>
    mutationFetch<{ status: string; durationMs?: number }>(
      "/-/resync",
      "POST",
      undefined,
      undefined,
      undefined,
      resyncTimeoutMilliseconds,
    ),
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
    ).then(({ data, ...rest }) => ({ data: data.plugin, ...rest })),
  pluginPrivileges: (remote: string) =>
    mutationFetch<{ privileges: PluginPrivilege[] }>(
      "/plugins/privileges",
      "POST",
      { remote },
      "application/json",
    ).then(({ privileges }) => privileges),
  installPlugin: (remote: string, privileges: PluginPrivilege[]) =>
    mutationFetch<{ plugin: Plugin }>(
      "/plugins",
      "POST",
      { remote, privileges },
      "application/json",
    ),
  enablePlugin: (name: string) => post<void>(`/plugins/${encodeURIComponent(name)}/enable`),
  disablePlugin: (name: string) => post<void>(`/plugins/${encodeURIComponent(name)}/disable`),
  removePlugin: (name: string, force?: boolean) =>
    del(`/plugins/${encodeURIComponent(name)}${force ? "?force=true" : ""}`),
  upgradePlugin: (name: string, remote: string, privileges: PluginPrivilege[]) =>
    mutationFetch<void>(
      `/plugins/${encodeURIComponent(name)}/upgrade`,
      "POST",
      { remote, privileges },
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
      ({ data, ...rest }) => ({ data: data.node, ...rest }),
    ),
  services: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<ServiceListItem>("/services", params, signal, schema.serviceListSchema),
  allServices: (params?: ListParams, signal?: AbortSignal) =>
    fetchAll<ServiceListItem>("/services", signal, schema.serviceListSchema, params),
  recommendations: () =>
    fetchJSON<RecommendationsResponse>(
      "/recommendations",
      undefined,
      schema.recommendationsSchema,
    ).then(({ data }) => data),
  service: (id: string, signal?: AbortSignal) =>
    fetchJSON<ServiceDetail>(`/services/${id}`, signal, schema.serviceDetailSchema),
  serviceCompose: (id: string, signal?: AbortSignal) =>
    fetchText(`/services/${encodeURIComponent(id)}`, signal, "application/yaml"),
  tasks: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Task>("/tasks", params, signal, schema.taskSchema),
  ports: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<PublishedPort>("/ports", params, signal, schema.publishedPortSchema),
  stacks: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Stack>("/stacks", params, signal, schema.stackListSchema),
  stacksSummary: () =>
    fetchJSON<CollectionResponse<StackSummary>>(
      "/stacks/summary",
      undefined,
      schema.collectionOf(schema.stackSummarySchema),
    ).then(({ data }) => data.items),
  stack: (name: string, signal?: AbortSignal) =>
    fetchJSON<StackResponse>(
      `/stacks/${encodeURIComponent(name)}`,
      signal,
      schema.stackDetailResponseSchema,
    ),
  stackCompose: (name: string, signal?: AbortSignal) =>
    fetchText(`/stacks/${encodeURIComponent(name)}`, signal, "application/yaml"),
  configs: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<ConfigListItem>("/configs", params, signal, schema.configListSchema),
  configVersions: (id: string, signal?: AbortSignal) =>
    fetchAll<ConfigListItem>("/configs", signal, schema.configListSchema, { versionsOf: id }),
  allConfigs: (signal?: AbortSignal) =>
    fetchAll<Config>("/configs", signal, schema.configSchema, { versions: "all" }),
  config: (id: string, signal?: AbortSignal) =>
    fetchJSON<ConfigDetail>(`/configs/${id}`, signal, schema.configDetailSchema),
  secrets: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<SecretListItem>("/secrets", params, signal, schema.secretListSchema),
  secretVersions: (id: string, signal?: AbortSignal) =>
    fetchAll<SecretListItem>("/secrets", signal, schema.secretListSchema, { versionsOf: id }),
  allSecrets: (signal?: AbortSignal) =>
    fetchAll<Secret>("/secrets", signal, schema.secretSchema, { versions: "all" }),
  secret: (id: string, signal?: AbortSignal) =>
    fetchJSON<SecretDetail>(`/secrets/${id}`, signal, schema.secretDetailSchema),
  secretEntry: (id: string, signal?: AbortSignal) =>
    fetchJSON<SecretEntry>(
      `/secret-entries/${encodeURIComponent(id)}`,
      signal,
      schema.secretEntrySchema,
    ).then(({ data }) => data),
  networks: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Network>("/networks", params, signal, schema.networkSchema),
  allNetworks: (signal?: AbortSignal) =>
    fetchAll<Network>("/networks", signal, schema.networkSchema),
  network: (id: string, signal?: AbortSignal) =>
    fetchJSON<NetworkDetail>(`/networks/${id}`, signal, schema.networkDetailSchema),
  volumes: (params?: ListParams, signal?: AbortSignal) =>
    fetchRange<Volume>("/volumes", params, signal, schema.volumeSchema),
  volume: (name: string, signal?: AbortSignal) =>
    fetchJSON<VolumeDetail>(
      `/volumes/${encodeURIComponent(name)}`,
      signal,
      schema.volumeDetailSchema,
    ),
  task: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ task: Task }>(`/tasks/${id}`, signal, schema.taskDetailSchema).then(
      ({ data, ...rest }) => ({ data: data.task, ...rest }),
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
    const { items } = await fetchHistoryPage(historyPath(params), signal);

    return items;
  },
  recentActivity: fetchRecentActivity,
  historyPath,
  historyPage: fetchHistoryPage,
  topology: () => fetchJGF<JGFDocument>("/topology", schema.jgfDocumentSchema),
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
  updateServiceEndpointMode: (id: string, mode: "vip" | "dnsrr", ifMatch?: string | undefined) =>
    put<ServiceDetail>(`/services/${id}/endpoint-mode`, { mode }, ifMatch),
  updateServiceImage: (id: string, image: string) =>
    put<ServiceDetail>(`/services/${id}/image`, { image }),
  rollbackService: (id: string) => post<ServiceDetail>(`/services/${id}/rollback`),
  restartService: (id: string) => post<ServiceDetail>(`/services/${id}/restart`),
  updateNodeAvailability: (id: string, availability: "active" | "drain" | "pause") =>
    put<{ node: Node }>(`/nodes/${id}/availability`, { availability }),
  removeTask: (id: string) => del(`/tasks/${id}`),
  removeService: (id: string) => del(`/services/${id}`),
  updateNodeRole: (id: string, role: "worker" | "manager", ifMatch?: string | undefined) =>
    put<{ node: Node }>(`/nodes/${id}/role`, { role }, ifMatch),
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
  completeSecretEntry: (elicitation: string, data: string) =>
    mutationFetch<SecretDetail>("/secrets", "POST", { data, elicitation }, "application/json"),
  patchConfigLabels: (id: string, ops: PatchOp[], ifMatch?: string | undefined) =>
    patch<{ labels: Record<string, string> }>(
      `/configs/${id}/labels`,
      ops,
      "application/json-patch+json",
      ifMatch,
    ).then(({ labels }) => labels),
  patchSecretLabels: (id: string, ops: PatchOp[], ifMatch?: string | undefined) =>
    patch<{ labels: Record<string, string> }>(
      `/secrets/${id}/labels`,
      ops,
      "application/json-patch+json",
      ifMatch,
    ).then(({ labels }) => labels),
  removeNetwork: (id: string) => del(`/networks/${id}`),
  removeVolume: (name: string) => del(`/volumes/${encodeURIComponent(name)}`),

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
  nodeDrainImpact: (id: string, signal?: AbortSignal) =>
    fetchJSON<DrainImpact>(`/nodes/${id}/drain-impact`, signal, schema.drainImpactSchema).then(
      ({ data }) => data,
    ),
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
  provenance: (kind: AttestedKind, id: string, signal?: AbortSignal) =>
    fetchJSON<ImageProvenance>(
      `/${kind}s/${encodeURIComponent(id)}/provenance`,
      signal,
      schema.imageProvenanceSchema,
    ).then(({ data }) => data),
  sbom: (kind: AttestedKind, id: string, platform?: string, signal?: AbortSignal) =>
    fetchJSON<ImageSBOM>(
      attestationPath(kind, id, "sbom", platform),
      signal,
      schema.imageSBOMSchema,
    ).then(({ data }) => data),
  dockerfile: (kind: AttestedKind, id: string, platform?: string, signal?: AbortSignal) =>
    fetchJSON<ImageDockerfile>(
      attestationPath(kind, id, "dockerfile", platform),
      signal,
      schema.imageDockerfileSchema,
    ).then(({ data }) => data),
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
  patchServiceEnv: (id: string, ops: PatchOp[], ifMatch?: string | undefined) =>
    patch<{ env: Record<string, string> }>(
      `/services/${id}/env`,
      ops,
      "application/json-patch+json",
      ifMatch,
    ).then(({ env }) => env),
  patchNodeLabels: (id: string, ops: PatchOp[], ifMatch?: string | undefined) =>
    patch<{ labels: Record<string, string> }>(
      `/nodes/${id}/labels`,
      ops,
      "application/json-patch+json",
      ifMatch,
    ).then(({ labels }) => labels),
  patchServiceLabels: (id: string, ops: PatchOp[], ifMatch?: string | undefined) =>
    patch<{ labels: Record<string, string> }>(
      `/services/${id}/labels`,
      ops,
      "application/json-patch+json",
      ifMatch,
    ).then(({ labels }) => labels),
  patchServiceResources: (id: string, partial: unknown, ifMatch?: string | undefined) =>
    patch<{ resources: Record<string, unknown> }>(
      `/services/${id}/resources`,
      partial,
      "application/merge-patch+json",
      ifMatch,
    ).then(({ resources }) => resources),
  putServiceHealthcheck: (id: string, healthcheck: Healthcheck, ifMatch?: string | undefined) =>
    put<{ healthcheck: Healthcheck }>(`/services/${id}/healthcheck`, healthcheck, ifMatch),

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

  putServicePlacement: (id: string, placement: Placement, ifMatch?: string | undefined) =>
    put<{ placement: Placement }>(`/services/${id}/placement`, placement, ifMatch),

  patchServicePorts: (id: string, ports: PortConfig[], ifMatch?: string | undefined) =>
    patch<{ ports: PortConfig[] }>(
      `/services/${id}/ports`,
      { ports },
      "application/merge-patch+json",
      ifMatch,
    ),
  patchServiceConfigs: (id: string, configs: ServiceConfigRef[], ifMatch?: string | undefined) =>
    patch<{ configs: ServiceConfigRef[] }>(
      `/services/${id}/configs`,
      { configs },
      "application/merge-patch+json",
      ifMatch,
    ),
  patchServiceSecrets: (id: string, secrets: ServiceSecretRef[], ifMatch?: string | undefined) =>
    patch<{ secrets: ServiceSecretRef[] }>(
      `/services/${id}/secrets`,
      { secrets },
      "application/merge-patch+json",
      ifMatch,
    ),
  patchServiceNetworks: (id: string, networks: ServiceNetworkRef[], ifMatch?: string | undefined) =>
    patch<{ networks: ServiceNetworkRef[] }>(
      `/services/${id}/networks`,
      { networks },
      "application/merge-patch+json",
      ifMatch,
    ),
  patchServiceMounts: (id: string, mounts: ServiceMount[], ifMatch?: string | undefined) =>
    patch<{ mounts: ServiceMount[] }>(
      `/services/${id}/mounts`,
      { mounts },
      "application/merge-patch+json",
      ifMatch,
    ),

  patchServiceUpdatePolicy: (
    id: string,
    partial: Record<string, unknown>,
    ifMatch?: string | undefined,
  ) =>
    patch<{ updatePolicy: UpdateConfig }>(
      `/services/${id}/update-policy`,
      partial,
      "application/merge-patch+json",
      ifMatch,
    ),

  patchServiceRollbackPolicy: (
    id: string,
    partial: Record<string, unknown>,
    ifMatch?: string | undefined,
  ) =>
    patch<{ rollbackPolicy: UpdateConfig }>(
      `/services/${id}/rollback-policy`,
      partial,
      "application/merge-patch+json",
      ifMatch,
    ),

  patchServiceLogDriver: (
    id: string,
    partial: Record<string, unknown>,
    ifMatch?: string | undefined,
  ) =>
    patch<{ logDriver: LogDriver }>(
      `/services/${id}/log-driver`,
      partial,
      "application/merge-patch+json",
      ifMatch,
    ),

  serviceContainerConfig: (id: string, signal?: AbortSignal) =>
    fetchJSON<{ containerConfig: ContainerConfig }>(
      `/services/${id}/container-config`,
      signal,
      schema.containerConfigSchema,
    ),

  patchServiceContainerConfig: (
    id: string,
    partial: Record<string, unknown>,
    ifMatch?: string | undefined,
  ) =>
    patch<{ containerConfig: ContainerConfig }>(
      `/services/${id}/container-config`,
      partial,
      "application/merge-patch+json",
      ifMatch,
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
