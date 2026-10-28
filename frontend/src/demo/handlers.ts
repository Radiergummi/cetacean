import { randomHex, type Dataset } from "./dataset";
import { handleInstantQuery, handleRangeQuery } from "./prometheus";
import { broadcast, type SSEClients } from "./sseHandlers";
import type { ClusterSnapshot, HealthInfo, LogResponse, ManagerQuorum } from "@/api/client";
import type {
  ClusterCapacity,
  CollectionResponse,
  Config,
  ConfigDetail,
  ContainerConfig,
  DiskUsageSummary,
  Healthcheck,
  HistoryEntry,
  LogDriver,
  MonitoringStatus,
  Network,
  NetworkAddresses,
  NetworkDetail,
  Node,
  Placement,
  Plugin,
  PortConfig,
  PrometheusResponse,
  RecommendationsResponse,
  SearchResourceType,
  SearchResponse,
  SearchResult,
  Secret,
  SecretDetail,
  Service,
  ServiceConfigRef,
  ServiceDetail,
  ServiceLink,
  ServiceListItem,
  ServiceMount,
  ServiceNetworkRef,
  ServiceRef,
  ServiceSecretRef,
  StackResponse,
  StackSummary,
  SwarmInfo,
  Task,
  UpdateConfig,
  VersionedListItem,
  Volume,
  VolumeDetail,
  JGFNode,
  JGFEdge,
  JGFHyperedge,
} from "@/api/types";
import { http, HttpResponse, type JsonBodyType, type PathParams } from "msw";

const stackLabel = "com.docker.stack.namespace";

/**
 * What the server would read from the dataset's `cetacean.link.*` labels,
 * written out rather than parsed.
 */
const demoServiceLinks: Record<string, ServiceLink[]> = {
  webshop_api: [
    { id: "grafana", title: "Dashboard", href: "https://grafana.example.com/d/webshop-api" },
    { id: "oncall", title: "On call", text: "pager:webshop-team" },
  ],
};

/**
 * The server's merge: exact duplicates become one link naming every service
 * that declares it, ordered by id, then value, then title.
 */
function stackLinks(services: Service[]): ServiceLink[] {
  const merged = new Map<string, ServiceLink & { services: string[] }>();

  for (const {
    Spec: { Name },
  } of services) {
    for (const link of demoServiceLinks[Name] ?? []) {
      const key = JSON.stringify([link.id, link.title, link.href, link.text]);
      const existing = merged.get(key);

      if (existing) {
        existing.services.push(Name);
      } else {
        merged.set(key, { ...link, services: [Name] });
      }
    }
  }

  const compare = (a = "", b = "") => (a < b ? -1 : a > b ? 1 : 0);
  const links: ServiceLink[] = [...merged.values()].map(({ services: names, ...link }) => ({
    ...link,
    services: names.sort(compare),
  }));

  return links.sort(
    (a, b) =>
      compare(a.id, b.id) ||
      compare(a.href, b.href) ||
      compare(a.text, b.text) ||
      compare(a.title, b.title),
  );
}

function jsonResponse<T extends JsonBodyType>(data: T, status = 200) {
  return HttpResponse.json(data, { status });
}

/**
 * The demo visitor is an operator at operations level 3 holding every grant, so
 * each path offers the writes the server routes at it, and links every
 * sub-resource beneath it that takes one.
 */
const writesByPath: [RegExp, string][] = [
  [/\/services\/[^/]+$/, "DELETE"],
  [/\/services\/[^/]+\/endpoint-mode$/, "PUT"],
  [/\/nodes\/[^/]+$/, "DELETE"],
  [/\/(configs|secrets)\/[^/]+$/, "DELETE"],
  [/\/(configs|secrets|plugins)$/, "POST"],
  [/\/plugins\/[^/]+$/, "DELETE"],
  [/\/(tasks|stacks|networks|volumes)\/[^/]+$/, "DELETE"],
];

export const editableByPath: [RegExp, string[]][] = [
  [
    /\/services\/[^/]+$/,
    [
      "scale",
      "image",
      "rollback",
      "restart",
      "env",
      "labels",
      "resources",
      "healthcheck",
      "placement",
      "ports",
      "update-policy",
      "rollback-policy",
      "log-driver",
      "configs",
      "secrets",
      "networks",
      "mounts",
      "container-config",
      "endpoint-mode",
    ],
  ],
  [/\/nodes\/[^/]+$/, ["availability", "role", "labels"]],
  [/\/(configs|secrets)\/[^/]+$/, ["labels"]],
  [/\/plugins\/[^/]+$/, ["enable", "disable", "settings", "upgrade"]],
  [
    /\/swarm$/,
    [
      "ca",
      "dispatcher",
      "encryption",
      "force-rotate-ca",
      "orchestration",
      "raft",
      "rotate-token",
      "rotate-unlock-key",
      "unlock",
    ],
  ],
];

function allowFor(url: string) {
  const { pathname } = new URL(url);
  const writes = writesByPath.find(([pattern]) => pattern.test(pathname))?.[1];

  return writes ? `GET, HEAD, ${writes}` : "GET, HEAD";
}

/** The `rel="edit"` links the server sends on a detail response. */
function editLinksFor(url: string) {
  const { pathname } = new URL(url);
  const names = editableByPath.find(([pattern]) => pattern.test(pathname))?.[1] ?? [];

  return names.map((name) => `<${pathname}/${name}>; rel="edit"`).join(", ");
}

/**
 * Registers a GET handler whose response carries the path's Allow.
 */
function get(
  path: string,
  resolver: (info: { params: PathParams; request: Request }) => Response | Promise<Response>,
) {
  return http.get(path, async (info) => {
    const response = await resolver(info);
    response.headers.set("Allow", allowFor(info.request.url));

    const links = editLinksFor(info.request.url);

    if (links) {
      response.headers.set("Link", links);
    }

    return response;
  });
}

function detailEnvelope<T extends object>(id: string, type: string, extra: T) {
  return { "@context": "/api/context.jsonld", "@id": id, "@type": type, ...extra };
}

function notFound() {
  return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
}

function createTask(service: Service, slot: number, node: Node): Task {
  return {
    ID: randomHex(25),
    Version: { Index: Math.floor(Math.random() * 10000) },
    ServiceID: service.ID,
    NodeID: node.ID,
    Slot: slot,
    Status: {
      Timestamp: new Date().toISOString(),
      State: "running",
      Message: "started",
      ContainerStatus: {
        ContainerID: randomHex(64),
        ExitCode: 0,
      },
    },
    DesiredState: "running",
    Spec: { ContainerSpec: { Image: service.Spec.TaskTemplate!.ContainerSpec?.Image ?? "" } },
    ServiceName: service.Spec.Name,
    NodeHostname: node.Description.Hostname,
  };
}

/**
 * Plain string comparison, the order the Go handler sorts in.
 */
function compareStrings(a: string, b: string): number {
  if (a === b) {
    return 0;
  }

  return a < b ? -1 : 1;
}

/**
 * Parse the Range header (e.g. "items=0-49", or the deprecated "items 0-49") into offset and limit.
 */
function parseRange(request: Request): { offset: number; limit: number } {
  const range = request.headers.get("Range");

  if (range) {
    const match = range.match(/^items(?:=|\s+)(\d+)-(\d+)$/i);

    if (match) {
      const start = parseInt(match[1] ?? "", 10);
      const end = parseInt(match[2] ?? "", 10);
      return { offset: start, limit: end - start + 1 };
    }
  }

  return { offset: 0, limit: 50 };
}

function wrapItem<T>(item: T, type: string, id: string): T & { "@id": string; "@type": string } {
  return { "@id": id, "@type": type, ...item };
}

function paginateWrapped<T>(
  items: T[],
  request: Request,
  type: string,
  idExtractor: (item: T) => string,
): CollectionResponse<T & { "@id": string; "@type": string }> {
  const { offset, limit } = parseRange(request);
  const sliced = items.slice(offset, offset + limit);
  return {
    items: sliced.map((item) => wrapItem(item, type, idExtractor(item))),
    total: items.length,
    limit,
    offset,
  };
}

function getStackName(labels: Record<string, string> | null): string | undefined {
  return labels?.[stackLabel];
}

function findServicesUsing(
  dataset: Dataset,
  predicate: (service: Service) => boolean,
): ServiceRef[] {
  return dataset.services
    .filter(predicate)
    .map((service) => ({ id: service.ID, name: service.Spec.Name }));
}

/**
 * The services whose spec or previous spec names a config or secret, as the
 * server lists them: one only its previous spec names is marked `rollback`.
 */
function servicesReferencing(
  dataset: Dataset,
  names: (spec: Service["Spec"] | undefined) => boolean,
): ServiceRef[] {
  return dataset.services.flatMap((service) => {
    if (names(service.Spec)) {
      return [{ id: service.ID, name: service.Spec.Name }];
    }

    if (names(service.PreviousSpec)) {
      return [{ id: service.ID, name: service.Spec.Name, rollback: true }];
    }

    return [];
  });
}

function servicesUsingConfig(dataset: Dataset, id: string): ServiceRef[] {
  return servicesReferencing(
    dataset,
    (spec) =>
      spec?.TaskTemplate?.ContainerSpec?.Configs?.some(({ ConfigID }) => ConfigID === id) ?? false,
  );
}

function servicesUsingSecret(dataset: Dataset, id: string): ServiceRef[] {
  return servicesReferencing(
    dataset,
    (spec) =>
      spec?.TaskTemplate?.ContainerSpec?.Secrets?.some(({ SecretID }) => SecretID === id) ?? false,
  );
}

const versionLabel = "cetacean.version-of";
const versionSuffix = /[_-](?:([0-9a-f]{8,})|v[0-9]+)$/i;

/**
 * A name less its version suffix, where a hash must hold a letter, as on the server.
 */
function stripVersionSuffix(name: string): string {
  const match = versionSuffix.exec(name);

  if (!match || (match[1] !== undefined && !/[a-f]/i.test(match[1]))) {
    return name;
  }

  return name.slice(0, match.index);
}

interface Versionable {
  ID: string;
  CreatedAt: string;
  Spec: { Name: string; Labels: Record<string, string> | null };
}

/**
 * The server's grouping of config and secret versions, as much of it as the
 * demo's data needs: a label as written, else the name less its version
 * suffix, either way less its stack prefix, within one stack.
 */
function versionKey({ Spec: { Name, Labels } }: Versionable): { stack: string; name: string } {
  const stack = Labels?.[stackLabel] ?? "";
  const name = Labels?.[versionLabel] || stripVersionSuffix(Name);
  const prefix = [`${stack}_`, `${stack}-`].find(
    (candidate) => stack && name.startsWith(candidate) && name.length > candidate.length,
  );

  return { stack, name: prefix ? name.slice(prefix.length) : name };
}

/**
 * Annotates every item with its place among its versions and its users.
 */
function annotateVersions<T extends Versionable>(
  items: T[],
  users: (id: string) => ServiceRef[],
): Array<T & VersionedListItem & { groupKey: string; rank: number }> {
  // An older version a current spec uses is shown; only the rest are counted.
  const inUse = (id: string) => users(id).some(({ rollback }) => !rollback);
  const groups = new Map<string, T[]>();

  for (const item of items) {
    const { stack, name } = versionKey(item);
    const key = `${stack}\u0000${name}`;
    groups.set(key, [...(groups.get(key) ?? []), item]);
  }

  return items.map((item) => {
    const { stack, name } = versionKey(item);
    const key = `${stack}\u0000${name}`;
    const members = [...(groups.get(key) ?? [])].sort(
      (left, right) =>
        Date.parse(right.CreatedAt) - Date.parse(left.CreatedAt) || right.ID.localeCompare(left.ID),
    );
    const rank = members.findIndex(({ ID }) => ID === item.ID);
    const grouped = members.length > 1;
    const hidden = members.filter(({ ID }, index) => index > 0 && !inUse(ID)).length;

    return {
      ...item,
      groupKey: grouped ? key : item.ID,
      rank,
      UsedBy: users(item.ID).length,
      // An ungrouped item is its own newest version, as on the server.
      NewestVersion: rank === 0,
      ...(grouped && {
        VersionOf: name,
        VersionInferred: members.some(({ Spec: { Labels } }) => !Labels?.[versionLabel]),
        ...(rank > 0 && inUse(item.ID) && { InUse: true }),
        ...(rank === 0 && hidden > 0 && { OlderVersions: hidden }),
      }),
    };
  });
}

/**
 * Applies `?versions` and `?versionsOf` the way the server does.
 */
function selectVersions<T extends Versionable>(
  items: T[],
  users: (id: string) => ServiceRef[],
  request: Request,
): Array<T & VersionedListItem> {
  const params = new URL(request.url).searchParams;
  const annotated = annotateVersions(items, users);
  const of = params.get("versionsOf");
  const versions = params.get("versions");
  const searching = Boolean(params.get("search")) || Boolean(params.get("filter"));
  const all = versions === "all" || (!versions && searching);
  let selected = annotated.filter(({ rank, InUse }) => all || rank === 0 || InUse);

  if (of) {
    const target = annotated.find(({ ID }) => ID === of);
    selected = annotated
      .filter(({ groupKey }) => groupKey === target?.groupKey)
      .sort((left, right) => left.rank - right.rank);
  }

  return selected.map(
    ({ groupKey: _groupKey, rank: _rank, ...item }) => item as T & VersionedListItem,
  );
}

/**
 * One item's version fields as a detail spells them.
 */
function versionDetail<T extends Versionable>(
  items: T[],
  id: string,
  users: (id: string) => ServiceRef[],
) {
  const item = annotateVersions(items, users).find(({ ID }) => ID === id);

  return {
    usedBy: item?.UsedBy ?? 0,
    ...(item?.NewestVersion && { newestVersion: true }),
    ...(item?.InUse && { inUse: true }),
    ...(item?.VersionOf && {
      versionOf: item.VersionOf,
      versionInferred: item.VersionInferred,
      olderVersions: item.OlderVersions,
    }),
  };
}

/**
 * Task states that still hold their addresses; past running they are freed.
 */
const addressHoldingStates = new Set([
  "new",
  "allocated",
  "pending",
  "assigned",
  "accepted",
  "preparing",
  "ready",
  "starting",
  "running",
]);

/**
 * The usable addresses of an IPv4 subnet, as the server counts them: less the
 * network and broadcast addresses up to a /30, and the gateway. The dataset
 * sets no IP range, and its gateways sit in the subnet.
 */
function usableAddresses(subnet: string): number | undefined {
  const [address, prefix] = subnet.split("/");
  const bits = Number(prefix);

  if (!address?.includes(".") || !Number.isInteger(bits) || bits < 0 || bits > 32) {
    return undefined;
  }

  const size = 2 ** (32 - bits);

  return bits <= 30 ? size - 3 : size - 1;
}

/**
 * A demo network's address figure, under the server's rules: only a swarm
 * network on the default IPAM driver with an IPv4 subnet has one. The dataset
 * records no VIPs or attachments, so a VIP-mode service stands in for its VIP
 * and a service's tasks for the addresses they hold.
 */
function demoAddresses(dataset: Dataset, network: Network): NetworkAddresses | undefined {
  const driver = network.IPAM.Driver;

  if (network.Scope !== "swarm" || (driver && driver !== "default")) {
    return undefined;
  }

  const pools = (network.IPAM.Config ?? []).flatMap(({ Subnet }) => {
    const usable = Subnet ? usableAddresses(Subnet) : undefined;

    return usable === undefined ? [] : [usable];
  });

  if (pools.length === 0) {
    return undefined;
  }

  const services = dataset.services.filter((service) =>
    network.Ingress
      ? (service.Endpoint?.Ports ?? []).some(({ PublishMode }) => PublishMode === "ingress")
      : (service.Spec.TaskTemplate?.Networks ?? []).some(({ Target }) => Target === network.Id),
  );
  const serviceIDs = new Set(services.map(({ ID }) => ID));
  const tasks = dataset.tasks.filter(
    ({ ServiceID, Status }) => serviceIDs.has(ServiceID) && addressHoldingStates.has(Status.State),
  );
  const vips = services.filter(({ Spec }) => Spec.EndpointSpec?.Mode !== "dnsrr").length;
  const loadBalancers = network.Ingress
    ? dataset.nodes.length
    : new Set(tasks.flatMap(({ NodeID }) => (NodeID ? [NodeID] : []))).size;

  return {
    usable: pools.reduce((total, usable) => total + usable, 0),
    used: vips + tasks.length + loadBalancers,
    vips,
    taskAddresses: tasks.length,
    loadBalancers,
    excludesAttachedContainers: network.Attachable,
  };
}

function deriveStacks(dataset: Dataset) {
  const stacks = new Map<
    string,
    {
      services: string[];
      configs: string[];
      secrets: string[];
      networks: string[];
      volumes: string[];
    }
  >();

  const ensureStack = (name: string) => {
    if (!stacks.has(name)) {
      stacks.set(name, { services: [], configs: [], secrets: [], networks: [], volumes: [] });
    }

    return stacks.get(name)!;
  };

  for (const service of dataset.services) {
    const name = getStackName(service.Spec.Labels);

    if (name) {
      ensureStack(name).services.push(service.ID);
    }
  }

  for (const config of dataset.configs) {
    const name = getStackName(config.Spec.Labels);

    if (name) {
      ensureStack(name).configs.push(config.ID);
    }
  }

  for (const secret of dataset.secrets) {
    const name = getStackName(secret.Spec.Labels);

    if (name) {
      ensureStack(name).secrets.push(secret.ID);
    }
  }

  for (const network of dataset.networks) {
    const name = getStackName(network.Labels);

    if (name) {
      ensureStack(name).networks.push(network.Id);
    }
  }

  for (const volume of dataset.volumes) {
    const name = getStackName(volume.Labels);

    if (name) {
      ensureStack(name).volumes.push(volume.Name);
    }
  }

  return stacks;
}

export function buildClusterSnapshot(dataset: Dataset): ClusterSnapshot {
  const tasksByState: Record<string, number> = {};

  for (const task of dataset.tasks) {
    tasksByState[task.Status.State] = (tasksByState[task.Status.State] || 0) + 1;
  }

  const stacks = deriveStacks(dataset);

  let totalCPU = 0;
  let totalMemory = 0;
  let nodesReady = 0;
  let nodesDown = 0;
  let nodesDraining = 0;
  let managers = 0;
  let reachableManagers = 0;

  for (const node of dataset.nodes) {
    if (node.ManagerStatus) {
      managers++;

      if (node.ManagerStatus.Reachability !== "unreachable") {
        reachableManagers++;
      }
    }

    totalCPU += node.Description.Resources.NanoCPUs ?? 0;
    totalMemory += node.Description.Resources.MemoryBytes ?? 0;

    if (node.Status.State === "ready") {
      nodesReady++;
    } else {
      nodesDown++;
    }

    if (node.Spec.Availability === "drain") {
      nodesDraining++;
    }
  }

  // A global service keeps its tasks on every ready node that is active or
  // paused, and starts new ones only on active nodes; the demo's services carry
  // no placement constraints.
  const readyNodes = dataset.nodes.filter(({ Status }) => Status.State === "ready");
  const eligibleNodes = readyNodes.filter(({ Spec }) => Spec.Availability === "active").length;
  const schedulableNodes = readyNodes.filter(
    ({ Spec }) => Spec.Availability === "active" || Spec.Availability === "pause",
  ).length;

  let reservedCPU = 0;
  let reservedMemory = 0;

  for (const service of dataset.services) {
    const reservations = service.Spec.TaskTemplate!.Resources?.Reservations;

    if (!reservations) {
      continue;
    }

    let replicas = 0;

    if (service.Spec.Mode.Replicated?.Replicas !== undefined) {
      replicas = service.Spec.Mode.Replicated.Replicas;
    } else if (service.Spec.Mode.Global) {
      replicas = schedulableNodes;
    }

    reservedCPU += (reservations.NanoCPUs ?? 0) * replicas;
    reservedMemory += (reservations.MemoryBytes ?? 0) * replicas;
  }

  let servicesConverged = 0;
  let servicesDegraded = 0;

  for (const service of dataset.services) {
    const state = demoServiceState(service, dataset.tasks, eligibleNodes);

    if (state === "converged") {
      servicesConverged++;
    }

    if (state === "degraded") {
      servicesDegraded++;
    }
  }

  return {
    nodeCount: dataset.nodes.length,
    serviceCount: dataset.services.length,
    taskCount: dataset.tasks.length,
    stackCount: stacks.size,
    tasksByState,
    nodesReady,
    nodesDown,
    nodesDraining,
    servicesConverged,
    servicesDegraded,
    reservedCPU,
    reservedMemory,
    totalCPU,
    totalMemory,
    quorum: quorumOf(managers, reachableManagers),
    prometheusConfigured: true,
  };
}

const updatingStates = new Set(["updating", "rollback_started", "rollback_paused"]);

/**
 * The server's overview rule, without restart tracking: a service mid-update
 * counts in neither bucket, a job converges once its run met its completion
 * target, and a global service with nowhere to run has nothing outstanding.
 * Running counts only tasks the orchestrator still means to run.
 */
function demoServiceState(
  service: Service,
  tasks: Task[],
  eligibleNodes: number,
): "converged" | "degraded" | "updating" {
  if (service.UpdateStatus && updatingStates.has(service.UpdateStatus.State ?? "")) {
    return "updating";
  }

  const own = tasks.filter(({ ServiceID }) => ServiceID === service.ID);
  const running = own.filter(
    ({ Status, DesiredState }) =>
      Status.State === "running" && DesiredState !== "shutdown" && DesiredState !== "remove",
  ).length;
  const { Mode } = service.Spec;

  if (Mode.ReplicatedJob || Mode.GlobalJob) {
    const completed = own.filter(({ Status }) => Status.State === "complete").length;
    const target = Mode.ReplicatedJob?.TotalCompletions ?? Mode.ReplicatedJob?.MaxConcurrent ?? 1;

    return running > 0 || completed >= Math.max(target, 1) ? "converged" : "degraded";
  }

  if (Mode.Global) {
    return running > 0 || eligibleNodes === 0 ? "converged" : "degraded";
  }

  const desired = Mode.Replicated?.Replicas ?? 0;

  return running >= desired ? "converged" : "degraded";
}

function quorumOf(managers: number, reachable: number): ManagerQuorum {
  if (managers === 0) {
    return { managers, reachable, size: 0, held: false, canLose: 0 };
  }

  const size = Math.floor(managers / 2) + 1;

  return {
    managers,
    reachable,
    size,
    held: reachable >= size,
    canLose: Math.max(reachable - size, 0),
  };
}

function buildStackSummaries(dataset: Dataset): StackSummary[] {
  const stacks = deriveStacks(dataset);
  const summaries: StackSummary[] = [];

  for (const [name, stack] of stacks) {
    const services = stack.services.map((id) => dataset.servicesByID.get(id)).filter(Boolean);

    let desiredTasks = 0;
    let updatingServices = 0;
    let memoryLimitBytes = 0;
    let cpuLimitCores = 0;
    const tasksByState: Record<string, number> = {};

    for (const service of services) {
      if (!service) {
        continue;
      }

      let replicas = 1;

      if (service.Spec.Mode.Replicated?.Replicas !== undefined) {
        replicas = service.Spec.Mode.Replicated.Replicas;
      } else if (service.Spec.Mode.Global) {
        replicas = dataset.nodes.length;
      }

      desiredTasks += replicas;

      if (service.UpdateStatus?.State === "updating" || service.UpdateStatus?.State === "paused") {
        updatingServices++;
      }

      const limits = service.Spec.TaskTemplate!.Resources?.Limits;

      if (limits) {
        memoryLimitBytes += (limits.MemoryBytes ?? 0) * replicas;
        cpuLimitCores += ((limits.NanoCPUs ?? 0) * replicas) / 1_000_000_000;
      }
    }

    for (const task of dataset.tasks) {
      const taskService = dataset.servicesByID.get(task.ServiceID);

      if (taskService && getStackName(taskService.Spec.Labels) === name) {
        tasksByState[task.Status.State] = (tasksByState[task.Status.State] || 0) + 1;
      }
    }

    summaries.push({
      name,
      serviceCount: stack.services.length,
      configCount: stack.configs.length,
      secretCount: stack.secrets.length,
      networkCount: stack.networks.length,
      volumeCount: stack.volumes.length,
      desiredTasks,
      tasksByState,
      updatingServices,
      memoryLimitBytes,
      cpuLimitCores,
      memoryUsageBytes: 0,
      cpuUsagePercent: 0,
    });
  }

  return summaries;
}

function searchDataset(
  dataset: Dataset,
  query: string,
  limit: number,
): {
  results: Partial<Record<SearchResourceType, SearchResult[]>>;
  counts: Partial<Record<SearchResourceType, number>>;
  total: number;
} {
  const lowerQuery = query.toLowerCase();
  const matches = (name: string): boolean => name.toLowerCase().includes(lowerQuery);

  const results: Partial<Record<SearchResourceType, SearchResult[]>> = {};
  const counts: Partial<Record<SearchResourceType, number>> = {};
  let total = 0;

  function collect(type: SearchResourceType, items: SearchResult[]) {
    if (items.length === 0) return;
    results[type] = limit > 0 ? items.slice(0, limit) : items;
    counts[type] = items.length;
    total += items.length;
  }

  collect(
    "services",
    dataset.services
      .filter((service) => {
        const image = service.Spec.TaskTemplate!.ContainerSpec?.Image ?? "";
        return matches(service.Spec.Name) || matches(image);
      })
      .map((service) => {
        const image = service.Spec.TaskTemplate!.ContainerSpec?.Image ?? "";

        return {
          id: service.ID,
          name: service.Spec.Name,
          detail: image.split("@")[0] ?? image,
        };
      }),
  );

  collect(
    "stacks",
    [...deriveStacks(dataset).entries()]
      .filter(([name]) => matches(name))
      .map(([name, stack]) => ({ id: name, name, detail: `${stack.services.length} services` })),
  );

  collect(
    "nodes",
    dataset.nodes
      .filter((node) => matches(node.Description.Hostname ?? ""))
      .map((node) => ({
        id: node.ID,
        name: node.Description.Hostname ?? node.ID,
        detail: `${node.Spec.Role} (${node.Status.State})`,
      })),
  );

  collect(
    "configs",
    dataset.configs
      .filter((config) => matches(config.Spec.Name))
      .map((config) => ({ id: config.ID, name: config.Spec.Name, detail: "" })),
  );

  collect(
    "secrets",
    dataset.secrets
      .filter((secret) => matches(secret.Spec.Name))
      .map((secret) => ({ id: secret.ID, name: secret.Spec.Name, detail: "" })),
  );

  collect(
    "networks",
    dataset.networks
      .filter((network) => matches(network.Name))
      .map((network) => ({
        id: network.Id,
        name: network.Name,
        detail: `${network.Driver} (${network.Scope})`,
      })),
  );

  collect(
    "volumes",
    dataset.volumes
      .filter((volume) => matches(volume.Name))
      .map((volume) => ({
        id: volume.Name,
        name: volume.Name,
        detail: `${volume.Driver} (${volume.Scope})`,
      })),
  );

  return { results, counts, total };
}

function countRunningTasks(dataset: Dataset, serviceID: string): number {
  return dataset.tasks.filter(
    (task) => task.ServiceID === serviceID && task.Status.State === "running",
  ).length;
}

/**
 * A service as the list serves it: its desired count is the server's rule, and
 * a job's outcome counts only its current run. The demo's services carry no
 * placement constraints, so a global service is eligible on every ready, active node.
 */
function serviceListItem(dataset: Dataset, service: Service): ServiceListItem {
  const { Mode } = service.Spec;
  const RunningTasks = countRunningTasks(dataset, service.ID);

  if (Mode.ReplicatedJob || Mode.GlobalJob) {
    const iteration = service.JobStatus?.JobIteration.Index;
    const run = dataset.tasks.filter(
      ({ ServiceID, JobIteration }) =>
        ServiceID === service.ID &&
        (iteration === undefined ||
          JobIteration?.Index === undefined ||
          JobIteration.Index === iteration),
    );
    const target = Mode.ReplicatedJob?.TotalCompletions ?? Mode.ReplicatedJob?.MaxConcurrent ?? 1;

    return {
      ...service,
      RunningTasks,
      DesiredTasks: Math.max(target, 1),
      CompletedTasks: run.filter(({ Status }) => Status.State === "complete").length,
      FailedTasks: run.filter(
        ({ Status }) => Status.State === "failed" || Status.State === "rejected",
      ).length,
    };
  }

  const DesiredTasks = Mode.Global
    ? dataset.nodes.filter(
        ({ Status, Spec }) => Status.State === "ready" && Spec.Availability === "active",
      ).length
    : (Mode.Replicated?.Replicas ?? 0);

  return { ...service, RunningTasks, DesiredTasks };
}

export function createHandlers(dataset: Dataset, clients: SSEClients) {
  function broadcastServiceUpdate(service: Service) {
    broadcast(clients, "service", "services", service.ID, {
      type: "service",
      action: "update",
      id: service.ID,
      resource: serviceListItem(dataset, service),
    });
  }

  return [
    // ---- Health & meta ----
    get("*/-/health", () => {
      const data: HealthInfo = {
        status: "ok",
        version: "demo",
        commit: "demo",
        buildDate: new Date().toISOString(),
        operationsLevel: 3,
      };
      return jsonResponse<HealthInfo>(data);
    }),

    get("*/-/ready", () => {
      return jsonResponse<{ status: string }>({ status: "ready" });
    }),

    // The frontend calls /profile for whoami
    get("*/profile", () => {
      return jsonResponse(
        detailEnvelope("/profile", "Profile", {
          subject: "demo",
          displayName: "Demo User",
          provider: "none",
        }),
      );
    }),

    get("*/auth/whoami", () => {
      return jsonResponse(
        detailEnvelope("/auth/whoami", "Identity", {
          subject: "demo",
          displayName: "Demo User",
          provider: "none",
        }),
      );
    }),

    // ---- Cluster ----
    get("*/cluster/metrics", () => {
      return jsonResponse(
        detailEnvelope("/cluster/metrics", "ClusterMetrics", {
          cpu: { used: 4.2, total: 16, percent: 26.25 },
          memory: { used: 12_884_901_888, total: 34_359_738_368, percent: 37.5 },
          disk: { used: 53_687_091_200, total: 214_748_364_800, percent: 25.0 },
        }),
      );
    }),

    get("*/cluster/capacity", () => {
      return jsonResponse<ClusterCapacity>({
        maxNodeCPU: 8_000_000_000,
        maxNodeMemory: 16 * 1024 * 1024 * 1024,
        totalCPU: 16_000_000_000,
        totalMemory: 32 * 1024 * 1024 * 1024,
        nodeCount: 3,
      });
    }),

    get("*/cluster", () => {
      return jsonResponse<ClusterSnapshot>(buildClusterSnapshot(dataset));
    }),

    // ---- Swarm ----
    get("*/swarm", () => {
      return jsonResponse<SwarmInfo>({
        swarm: dataset.swarm,
        managerAddr: "10.0.0.1:2377",
      });
    }),

    // ---- Topology (must be before */networks and */nodes to avoid glob conflicts) ----
    get("*/topology", () => {
      const context = "/api/context.jsonld";
      const urn = (type: string, id: string) => `urn:cetacean:${type}:${id}`;

      // Overlays only, matching cluster.OverlayNetworks — the bridge and host
      // networks carry no service-to-service connectivity.
      const overlays = new Map(
        dataset.networks
          .filter(({ Driver }) => Driver === "overlay")
          .map((network) => [network.Id, network]),
      );

      // ---- network graph ----
      const networkNodes: Record<string, JGFNode> = {};
      const serviceNetworks = new Map<string, string[]>();
      const stacks = new Map<string, string[]>();

      for (const service of dataset.services) {
        const serviceUrn = urn("service", service.ID);
        const ports = (service.Spec.EndpointSpec?.Ports ?? []).map(
          ({ PublishedPort, TargetPort, Protocol }) => `${PublishedPort}:${TargetPort}/${Protocol}`,
        );

        networkNodes[serviceUrn] = {
          label: service.Spec.Name,
          metadata: {
            "@context": context,
            kind: "service",
            replicas:
              service.Spec.Mode.Replicated?.Replicas ??
              (service.Spec.Mode.Global ? dataset.nodes.length : 1),
            runningReplicas: countRunningTasks(dataset, service.ID),
            image: (service.Spec.TaskTemplate!.ContainerSpec?.Image ?? "").split("@")[0],
            mode: service.Spec.Mode.Replicated ? "replicated" : "global",
            ...(ports.length > 0 ? { ports } : {}),
            ...(service.UpdateStatus ? { updateStatus: service.UpdateStatus.State } : {}),
          },
        };

        // The union cluster.ServiceAttachments takes: a dnsrr service has no
        // virtual IP, so the task template is the only record of its networks.
        const attached = new Set<string>();

        for (const { NetworkID } of service.Endpoint?.VirtualIPs ?? []) {
          if (NetworkID && overlays.has(NetworkID)) {
            attached.add(NetworkID);
          }
        }

        for (const { Target } of service.Spec.TaskTemplate!.Networks ?? []) {
          if (Target && overlays.has(Target)) {
            attached.add(Target);
          }
        }

        serviceNetworks.set(service.ID, [...attached]);

        const stack = service.Spec.Labels?.[stackLabel];

        if (stack) {
          stacks.set(stack, [...(stacks.get(stack) ?? []), serviceUrn]);
        }
      }

      const networkEdges: JGFEdge[] = [];
      const serviceIds = [...serviceNetworks.keys()].sort();

      for (let i = 0; i < serviceIds.length; i++) {
        for (let j = i + 1; j < serviceIds.length; j++) {
          const a = serviceIds[i]!;
          const b = serviceIds[j]!;
          const shared = (serviceNetworks.get(a) ?? []).filter((id) =>
            (serviceNetworks.get(b) ?? []).includes(id),
          );

          if (shared.length === 0) {
            continue;
          }

          networkEdges.push({
            source: urn("service", a),
            target: urn("service", b),
            metadata: {
              "@context": context,
              networks: shared.map((id) => {
                const network = overlays.get(id)!;

                return {
                  id: urn("network", id),
                  name: network.Name,
                  driver: network.Driver,
                  scope: network.Scope,
                };
              }),
            },
          });
        }
      }

      const networkHyperedges: JGFHyperedge[] = [...stacks.entries()]
        .sort(([a], [b]) => (a < b ? -1 : 1))
        .map(([name, members]) => ({
          nodes: [...members].sort(),
          metadata: { "@context": context, kind: "stack", name },
        }));

      // ---- placement graph ----
      const placementNodes: Record<string, JGFNode> = {};
      const placements = new Map<
        string,
        { nodeUrns: Set<string>; tasks: Record<string, unknown>[] }
      >();

      for (const node of dataset.nodes) {
        const nodeUrn = urn("node", node.ID);
        placementNodes[nodeUrn] = {
          label: node.Description.Hostname ?? node.ID,
          metadata: {
            "@context": context,
            kind: "node",
            role: node.Spec.Role,
            state: node.Status.State,
            availability: node.Spec.Availability,
            address: node.Status.Addr,
          },
        };

        for (const task of dataset.tasks) {
          // cluster.TaskIsLive: a replaced task still sits on its node, and
          // counting it reports the same replica twice.
          if (
            task.NodeID !== node.ID ||
            task.DesiredState === "shutdown" ||
            task.DesiredState === "remove"
          ) {
            continue;
          }

          let placement = placements.get(task.ServiceID);

          if (!placement) {
            placement = { nodeUrns: new Set(), tasks: [] };
            placements.set(task.ServiceID, placement);
          }

          placement.nodeUrns.add(nodeUrn);
          placement.tasks.push({
            id: urn("task", task.ID),
            node: nodeUrn,
            state: task.Status.State,
            slot: task.Slot ?? 0,
            image: (task.Spec.ContainerSpec?.Image ?? "").split("@")[0],
          });
        }
      }

      const placementHyperedges: JGFHyperedge[] = [];

      for (const [serviceId, { nodeUrns, tasks }] of placements) {
        const service = dataset.servicesByID.get(serviceId);
        const serviceUrn = urn("service", serviceId);

        placementNodes[serviceUrn] = {
          label: service?.Spec.Name ?? serviceId,
          metadata: {
            "@context": context,
            kind: "service",
            image: (service?.Spec.TaskTemplate?.ContainerSpec?.Image ?? "").split("@")[0],
            networks: (serviceNetworks.get(serviceId) ?? [])
              .map((id) => ({ id: urn("network", id), name: overlays.get(id)?.Name ?? id }))
              .sort((a, b) => compareStrings(a.name, b.name) || compareStrings(a.id, b.id)),
          },
        };

        placementHyperedges.push({
          nodes: [serviceUrn, ...[...nodeUrns].sort()],
          metadata: { "@context": context, kind: "placement", tasks },
        });
      }

      placementHyperedges.sort((a, b) => (a.nodes[0]! < b.nodes[0]! ? -1 : 1));

      return jsonResponse({
        graphs: [
          {
            id: "network",
            type: "network-topology",
            label: "Network Topology",
            directed: false,
            metadata: { "@context": context },
            nodes: networkNodes,
            edges: networkEdges,
            hyperedges: networkHyperedges,
          },
          {
            id: "placement",
            type: "placement-topology",
            label: "Placement Topology",
            directed: false,
            metadata: { "@context": context },
            nodes: placementNodes,
            hyperedges: placementHyperedges,
          },
        ],
      });
    }),

    // ---- Nodes ----
    get("*/nodes/:id/tasks", ({ params, request }) => {
      const nodeID = params.id as string;
      const nodeTasks = dataset.tasks.filter((task) => task.NodeID === nodeID);
      return jsonResponse(paginateWrapped(nodeTasks, request, "Task", (t) => `/tasks/${t.ID}`));
    }),

    get("*/nodes/:id/labels", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ labels: Record<string, string> }>({ labels: node.Spec.Labels ?? {} });
    }),

    get("*/nodes/:id/role", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const managerCount = dataset.nodes.filter((node) => node.Spec.Role === "manager").length;
      return jsonResponse(
        detailEnvelope(`/nodes/${params.id as string}/role`, "NodeRole", {
          role: node.Spec.Role,
          isLeader: node.ManagerStatus?.Leader ?? false,
          managerCount,
        }),
      );
    }),

    get("*/nodes/:id", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse(detailEnvelope(`/nodes/${node.ID}`, "Node", { node }));
    }),

    get("*/nodes", ({ request }) => {
      return jsonResponse(paginateWrapped(dataset.nodes, request, "Node", (n) => `/nodes/${n.ID}`));
    }),

    // ---- Services ----
    get("*/services/:id/tasks", ({ params, request }) => {
      const serviceID = params.id as string;
      const serviceTasks = dataset.tasks.filter((task) => task.ServiceID === serviceID);
      return jsonResponse(paginateWrapped(serviceTasks, request, "Task", (t) => `/tasks/${t.ID}`));
    }),

    get("*/services/:id/env", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const envArray = service.Spec.TaskTemplate!.ContainerSpec?.Env ?? [];
      const env: Record<string, string> = {};

      for (const entry of envArray) {
        const equalsIndex = entry.indexOf("=");

        if (equalsIndex >= 0) {
          env[entry.slice(0, equalsIndex)] = entry.slice(equalsIndex + 1);
        }
      }

      return jsonResponse<{ env: Record<string, string> }>({ env });
    }),

    get("*/services/:id/labels", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ labels: Record<string, string> }>({
        labels: service.Spec.Labels ?? {},
      });
    }),

    get("*/services/:id/resources", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ resources: Record<string, unknown> }>({
        resources: service.Spec.TaskTemplate!.Resources ?? {},
      });
    }),

    get("*/services/:id/healthcheck", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ healthcheck: Healthcheck | null }>({
        healthcheck: service.Spec.TaskTemplate!.ContainerSpec?.Healthcheck ?? null,
      });
    }),

    get("*/services/:id/configs", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const configs = (service.Spec.TaskTemplate!.ContainerSpec?.Configs ?? []).map(
        ({ ConfigID, ConfigName, File }) => ({
          configID: ConfigID,
          configName: ConfigName,
          fileName: File?.Name ?? "",
        }),
      );
      return jsonResponse<{ configs: ServiceConfigRef[] }>({ configs });
    }),

    get("*/services/:id/secrets", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const secrets = (service.Spec.TaskTemplate!.ContainerSpec?.Secrets ?? []).map(
        ({ SecretID, SecretName, File }) => ({
          secretID: SecretID,
          secretName: SecretName,
          fileName: File?.Name ?? "",
        }),
      );
      return jsonResponse<{ secrets: ServiceSecretRef[] }>({ secrets });
    }),

    get("*/services/:id/networks", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const networks = (service.Spec.TaskTemplate!.Networks ?? []).flatMap(({ Target, Aliases }) =>
        Target ? [{ target: Target, aliases: Aliases ?? undefined }] : [],
      );
      return jsonResponse<{ networks: ServiceNetworkRef[] }>({ networks });
    }),

    get("*/services/:id/mounts", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ mounts: ServiceMount[] }>({
        mounts: service.Spec.TaskTemplate!.ContainerSpec?.Mounts ?? [],
      });
    }),

    get("*/services/:id/ports", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ ports: PortConfig[] }>({
        ports: service.Spec.EndpointSpec?.Ports ?? [],
      });
    }),

    get("*/services/:id/placement", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ placement: Placement }>({
        placement: service.Spec.TaskTemplate!.Placement ?? {},
      });
    }),

    get("*/services/:id/update-policy", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ updatePolicy: Partial<UpdateConfig> }>({
        updatePolicy: service.Spec.UpdateConfig ?? {},
      });
    }),

    get("*/services/:id/rollback-policy", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ rollbackPolicy: Partial<UpdateConfig> }>({
        rollbackPolicy: service.Spec.RollbackConfig ?? {},
      });
    }),

    get("*/services/:id/log-driver", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ logDriver: Partial<LogDriver> }>({
        logDriver: service.Spec.TaskTemplate!.LogDriver ?? {},
      });
    }),

    get("*/services/:id/container-config", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const container = service.Spec.TaskTemplate!.ContainerSpec;
      return jsonResponse<{ containerConfig: ContainerConfig }>({
        containerConfig: {
          command: container?.Command ?? [],
          args: container?.Args ?? [],
          dir: container?.Dir ?? "",
          user: container?.User ?? "",
          hostname: container?.Hostname ?? "",
          init: container?.Init,
          tty: container?.TTY ?? false,
          readOnly: container?.ReadOnly ?? false,
          stopSignal: container?.StopSignal ?? "",
          stopGracePeriod: container?.StopGracePeriod,
          capabilityAdd: container?.CapabilityAdd ?? [],
          capabilityDrop: container?.CapabilityDrop ?? [],
          groups: container?.Groups ?? [],
          hosts: container?.Hosts ?? [],
          dnsConfig: container?.DNSConfig
            ? {
                nameservers: container.DNSConfig.Nameservers ?? undefined,
                search: container.DNSConfig.Search ?? undefined,
                options: container.DNSConfig.Options ?? undefined,
              }
            : undefined,
        },
      });
    }),

    get("*/services/:id/logs", () => {
      return jsonResponse<LogResponse>({ lines: [], oldest: "", newest: "", hasMore: false });
    }),

    get("*/services/:id", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse(
        detailEnvelope(`/services/${service.ID}`, "Service", {
          service,
          changes: [],
          integrations: [],
          ...(demoServiceLinks[service.Spec.Name] && {
            links: demoServiceLinks[service.Spec.Name],
          }),
        } satisfies ServiceDetail),
      );
    }),

    get("*/services", ({ request }) => {
      const items = dataset.services.map((service) => serviceListItem(dataset, service));
      return jsonResponse(paginateWrapped(items, request, "Service", (s) => `/services/${s.ID}`));
    }),

    // ---- Tasks ----
    get("*/tasks/:id/logs", () => {
      return jsonResponse<LogResponse>({ lines: [], oldest: "", newest: "", hasMore: false });
    }),

    get("*/tasks/:id", ({ params }) => {
      const task = dataset.tasksByID.get(params.id as string);

      if (!task) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse(detailEnvelope(`/tasks/${task.ID}`, "Task", { task }));
    }),

    get("*/tasks", ({ request }) => {
      return jsonResponse(paginateWrapped(dataset.tasks, request, "Task", (t) => `/tasks/${t.ID}`));
    }),

    // ---- Stacks ----
    get("*/stacks/summary", () => {
      const items = buildStackSummaries(dataset);
      return jsonResponse<CollectionResponse<StackSummary & { "@id": string; "@type": string }>>({
        items: items.map((summary) => wrapItem(summary, "StackSummary", `/stacks/${summary.name}`)),
        total: items.length,
        limit: 50,
        offset: 0,
      });
    }),

    get("*/stacks/:name", ({ params }) => {
      const stackName = params.name as string;
      const stacks = deriveStacks(dataset);
      const stack = stacks.get(stackName);

      if (!stack) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const services = stack.services
        .map((id) => dataset.servicesByID.get(id))
        .filter(Boolean) as Service[];
      const links = stackLinks(services);

      return jsonResponse(
        detailEnvelope(`/stacks/${stackName}`, "Stack", {
          stack: {
            name: stackName,
            services,
            configs: stack.configs
              .map((id) => dataset.configsByID.get(id))
              .filter(Boolean) as Config[],
            secrets: stack.secrets
              .map((id) => dataset.secretsByID.get(id))
              .filter(Boolean) as Secret[],
            networks: stack.networks
              .map((id) => dataset.networksByID.get(id))
              .filter(Boolean) as Network[],
            volumes: stack.volumes
              .map((name) => dataset.volumesByName.get(name))
              .filter(Boolean) as Volume[],
          },
          ...(links.length > 0 && { links }),
        } satisfies StackResponse),
      );
    }),

    get("*/stacks", ({ request }) => {
      const stacks = deriveStacks(dataset);
      const items = Array.from(stacks.entries()).map(([name, stack]) => ({
        name,
        services: stack.services,
        configs: stack.configs,
        secrets: stack.secrets,
        networks: stack.networks,
        volumes: stack.volumes,
      }));
      return jsonResponse(paginateWrapped(items, request, "Stack", (s) => `/stacks/${s.name}`));
    }),

    // ---- Configs ----
    get("*/configs/:id", ({ params }) => {
      const config = dataset.configsByID.get(params.id as string);

      if (!config) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const services = servicesUsingConfig(dataset, config.ID);

      return jsonResponse(
        detailEnvelope(`/configs/${config.ID}`, "Config", {
          config,
          services,
          ...versionDetail(dataset.configs, config.ID, (id) => servicesUsingConfig(dataset, id)),
        } satisfies ConfigDetail),
      );
    }),

    get("*/configs", ({ request }) => {
      const items = selectVersions(
        dataset.configs,
        (id) => servicesUsingConfig(dataset, id),
        request,
      );

      return jsonResponse(paginateWrapped(items, request, "Config", (c) => `/configs/${c.ID}`));
    }),

    // ---- Secrets ----
    get("*/secrets/:id", ({ params }) => {
      const secret = dataset.secretsByID.get(params.id as string);

      if (!secret) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const services = servicesUsingSecret(dataset, secret.ID);

      return jsonResponse(
        detailEnvelope(`/secrets/${secret.ID}`, "Secret", {
          secret,
          services,
          ...versionDetail(dataset.secrets, secret.ID, (id) => servicesUsingSecret(dataset, id)),
        } satisfies SecretDetail),
      );
    }),

    get("*/secrets", ({ request }) => {
      const items = selectVersions(
        dataset.secrets,
        (id) => servicesUsingSecret(dataset, id),
        request,
      );

      return jsonResponse(paginateWrapped(items, request, "Secret", (s) => `/secrets/${s.ID}`));
    }),

    // ---- Networks ----
    get("*/networks/:id", ({ params }) => {
      const network = dataset.networksByID.get(params.id as string);

      if (!network) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse(
        detailEnvelope(`/networks/${network.Id}`, "Network", {
          network,
          services: findServicesUsing(
            dataset,
            (service) =>
              service.Spec.TaskTemplate!.Networks?.some(({ Target }) => Target === network.Id) ??
              false,
          ),
          addresses: demoAddresses(dataset, network),
        } satisfies NetworkDetail),
      );
    }),

    get("*/networks", ({ request }) => {
      return jsonResponse(
        paginateWrapped(
          dataset.networks.map((network) => ({
            ...network,
            Addresses: demoAddresses(dataset, network),
          })),
          request,
          "Network",
          (n) => `/networks/${n.Id}`,
        ),
      );
    }),

    // ---- Volumes ----
    get("*/volumes/:name", ({ params }) => {
      const volume = dataset.volumesByName.get(params.name as string);

      if (!volume) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse(
        detailEnvelope(`/volumes/${volume.Name}`, "Volume", {
          volume,
          services: findServicesUsing(
            dataset,
            (service) =>
              service.Spec.TaskTemplate!.ContainerSpec?.Mounts?.some(
                ({ Source }) => Source === volume.Name,
              ) ?? false,
          ),
        } satisfies VolumeDetail),
      );
    }),

    get("*/volumes", ({ request }) => {
      return jsonResponse(
        paginateWrapped(dataset.volumes, request, "Volume", (v) => `/volumes/${v.Name}`),
      );
    }),

    // ---- Search ----
    get("*/search", ({ request }) => {
      const url = new URL(request.url);
      const query = url.searchParams.get("q") ?? "";
      const limitParam = url.searchParams.get("limit");
      const limit = limitParam !== null ? parseInt(limitParam, 10) : 3;

      const { results, counts, total } = searchDataset(dataset, query, limit);
      return jsonResponse<SearchResponse>({ query, results, counts, total });
    }),

    // ---- History ----
    get("*/history", () => {
      return jsonResponse<CollectionResponse<HistoryEntry & { "@id": string; "@type": string }>>({
        items: [],
        total: 0,
        limit: 50,
        offset: 0,
      });
    }),

    // ---- Recommendations ----
    get("*/recommendations", () => {
      return jsonResponse<RecommendationsResponse>({
        items: [],
        total: 0,
        summary: { critical: 0, warning: 0, info: 0 },
        computedAt: new Date().toISOString(),
      });
    }),

    // ---- Disk usage ----
    get("*/disk-usage", () => {
      const summaries: DiskUsageSummary[] = [
        { type: "images", count: 11, active: 11, totalSize: 3_221_225_472, reclaimable: 0 },
        {
          type: "containers",
          count: 25,
          active: 23,
          totalSize: 524_288_000,
          reclaimable: 104_857_600,
        },
        {
          type: "volumes",
          count: 2,
          active: 2,
          totalSize: 1_073_741_824,
          reclaimable: 0,
        },
        {
          type: "buildCache",
          count: 0,
          active: 0,
          totalSize: 0,
          reclaimable: 0,
        },
      ];
      return jsonResponse<
        CollectionResponse<DiskUsageSummary & { "@id": string; "@type": string }>
      >({
        items: summaries.map((summary) =>
          wrapItem(summary, "DiskUsageSummary", `/disk-usage#${summary.type}`),
        ),
        total: summaries.length,
        limit: 50,
        offset: 0,
      });
    }),

    // ---- Plugins ----
    get("*/plugins", () => {
      return jsonResponse<CollectionResponse<Plugin & { "@id": string; "@type": string }>>({
        items: [],
        total: 0,
        limit: 50,
        offset: 0,
      });
    }),

    // ---- Monitoring status ----
    get("*/metrics/status", () => {
      return jsonResponse<MonitoringStatus>({
        prometheusConfigured: true,
        prometheusReachable: true,
        queryable: true,
        nodeExporter: { targets: 3, nodes: 3 },
        cadvisor: { targets: 3, nodes: 3 },
      });
    }),

    // ---- Metrics (Prometheus proxy) ----
    get("*/metrics/labels/:name", ({ params }) => {
      const name = params.name as string;
      const valueMap: Record<string, string[]> = {
        __name__: [
          "container_cpu_usage_seconds_total",
          "container_memory_usage_bytes",
          "node_filesystem_avail_bytes",
          "node_memory_MemAvailable_bytes",
          "up",
        ],
        name: dataset.services.map((service) => service.Spec.Name),
        instance: dataset.nodes.map((node) => `${node.Description.Hostname}:9100`),
        job: ["cadvisor", "node-exporter", "prometheus"],
        nodename: dataset.nodes.map((node) => node.Description.Hostname ?? ""),
      };
      return jsonResponse<{ data: string[] }>({ data: valueMap[name] ?? [] });
    }),

    get("*/metrics/labels", () => {
      return jsonResponse<{ data: string[] }>({
        data: [
          "__name__",
          "container_label_com_docker_swarm_service_name",
          "device",
          "instance",
          "job",
          "mountpoint",
          "name",
          "nodename",
        ],
      });
    }),

    get("*/metrics", ({ request }) => {
      const url = new URL(request.url);
      const query = url.searchParams.get("query") ?? "";
      const start = url.searchParams.get("start");
      const end = url.searchParams.get("end");
      const step = url.searchParams.get("step");

      if (start && end && step) {
        const data = handleRangeQuery(
          query,
          parseFloat(start),
          parseFloat(end),
          parseFloat(step),
          dataset,
        );
        return jsonResponse<PrometheusResponse>({ data });
      }

      const data = handleInstantQuery(query, dataset);
      return jsonResponse<PrometheusResponse>({ data });
    }),

    // ---- Docker latest version ----
    get("*/-/docker-latest-version", () => {
      return jsonResponse<{ version: string; url: string }>({
        version: "27.5.1",
        url: "https://docs.docker.com/engine/release-notes/",
      });
    }),

    // ---- Write: Service lifecycle ----
    http.put("*/services/:id/scale", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as { replicas: number };
      const replicated = service.Spec.Mode.Replicated;

      if (!replicated) {
        return HttpResponse.json(
          { title: "Service is not replicated", status: 400 },
          { status: 400 },
        );
      }

      const oldReplicas = replicated.Replicas ?? 0;
      replicated.Replicas = body.replicas;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();

      if (body.replicas > oldReplicas) {
        for (let slot = oldReplicas + 1; slot <= body.replicas; slot++) {
          const workers = dataset.nodes.filter((node) => node.Spec.Role === "worker");
          const node = workers[slot % workers.length] ?? dataset.nodes[0];

          if (!node) {
            continue;
          }

          const task = createTask(service, slot, node);
          dataset.tasks.push(task);
          dataset.tasksByID.set(task.ID, task);
          broadcast(clients, "task", "tasks", task.ID, {
            type: "task",
            action: "create",
            id: task.ID,
            resource: task,
          });
        }
      } else if (body.replicas < oldReplicas) {
        for (const task of dataset.tasks) {
          if (
            task.ServiceID === service.ID &&
            task.Status.State === "running" &&
            (task.Slot ?? 0) > body.replicas
          ) {
            task.Status.State = "shutdown";
            task.DesiredState = "shutdown";
            task.Status.Timestamp = new Date().toISOString();
            broadcast(clients, "task", "tasks", task.ID, {
              type: "task",
              action: "update",
              id: task.ID,
              resource: task,
            });
          }
        }
      }

      broadcastServiceUpdate(service);
      return jsonResponse<Service>(service);
    }),

    http.put("*/services/:id/image", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as { image: string };

      service.PreviousSpec = JSON.parse(JSON.stringify(service.Spec));

      if (service.Spec.TaskTemplate!.ContainerSpec) {
        service.Spec.TaskTemplate!.ContainerSpec.Image = body.image;
      }

      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      service.UpdateStatus = {
        State: "completed",
        CompletedAt: new Date().toISOString(),
        Message: "update completed",
      };

      broadcastServiceUpdate(service);
      return jsonResponse<Service>(service);
    }),

    http.post("*/services/:id/rollback", async ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      if (service.PreviousSpec) {
        const current = JSON.parse(JSON.stringify(service.Spec));
        service.Spec = service.PreviousSpec;
        service.PreviousSpec = current;
      }

      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      service.UpdateStatus = {
        State: "rollback_completed",
        CompletedAt: new Date().toISOString(),
        Message: "rollback completed",
      };

      broadcastServiceUpdate(service);
      return jsonResponse<Service>(service);
    }),

    http.post("*/services/:id/restart", async ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse<Service>(service);
    }),

    http.delete("*/services/:id", async ({ params }) => {
      const id = params.id as string;
      const service = dataset.servicesByID.get(id);

      if (!service) {
        return notFound();
      }

      dataset.services = dataset.services.filter((service) => service.ID !== id);
      dataset.servicesByID.delete(id);

      for (const task of dataset.tasks) {
        if (task.ServiceID === id) {
          task.Status.State = "shutdown";
          task.DesiredState = "shutdown";
        }
      }

      broadcast(clients, "service", "services", id, { type: "service", action: "remove", id });
      return new HttpResponse(null, { status: 204 });
    }),

    // ---- Write: Node operations ----
    http.put("*/nodes/:id/availability", async ({ params, request }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return notFound();
      }

      const body = (await request.json()) as { availability: string };
      node.Spec.Availability = body.availability;
      node.Version.Index++;
      broadcast(clients, "node", "nodes", node.ID, {
        type: "node",
        action: "update",
        id: node.ID,
        resource: node,
      });
      return jsonResponse<Node>(node);
    }),

    http.patch("*/nodes/:id/labels", async ({ params, request }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return notFound();
      }

      const body = (await request.json()) as Record<string, string | null>;

      if (!node.Spec.Labels) {
        node.Spec.Labels = {};
      }

      for (const [key, value] of Object.entries(body)) {
        if (value === null) {
          delete node.Spec.Labels[key];
        } else {
          node.Spec.Labels[key] = value;
        }
      }

      node.Version.Index++;
      broadcast(clients, "node", "nodes", node.ID, {
        type: "node",
        action: "update",
        id: node.ID,
        resource: node,
      });
      return jsonResponse(
        detailEnvelope(`/nodes/${params.id}/labels`, "NodeLabels", {
          labels: node.Spec.Labels,
        }),
      );
    }),

    // ---- Write: Task operations ----
    http.delete("*/tasks/:id", async ({ params }) => {
      const id = params.id as string;
      const task = dataset.tasksByID.get(id);

      if (!task) {
        return notFound();
      }

      task.Status.State = "shutdown";
      task.DesiredState = "shutdown";
      task.Status.Timestamp = new Date().toISOString();
      broadcast(clients, "task", "tasks", id, {
        type: "task",
        action: "update",
        id,
        resource: task,
      });
      return new HttpResponse(null, { status: 204 });
    }),

    // ---- Write: Service spec PATCH operations ----
    http.patch("*/services/:id/env", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as Record<string, string | null>;
      const container = service.Spec.TaskTemplate!.ContainerSpec;

      if (!container) {
        return notFound();
      }

      const envMap = new Map<string, string>();

      for (const entry of container.Env ?? []) {
        const eq = entry.indexOf("=");

        if (eq >= 0) {
          envMap.set(entry.slice(0, eq), entry.slice(eq + 1));
        }
      }

      for (const [key, value] of Object.entries(body)) {
        if (value === null) {
          envMap.delete(key);
        } else {
          envMap.set(key, value);
        }
      }

      container.Env = [...envMap.entries()].map(([k, v]) => `${k}=${v}`);
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse(
        detailEnvelope(`/services/${params.id}/env`, "ServiceEnv", {
          env: Object.fromEntries(envMap),
        }),
      );
    }),

    http.patch("*/services/:id/labels", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as Record<string, string | null>;

      if (!service.Spec.Labels) {
        service.Spec.Labels = {};
      }

      for (const [key, value] of Object.entries(body)) {
        if (value === null) {
          delete service.Spec.Labels[key];
        } else {
          service.Spec.Labels[key] = value;
        }
      }

      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse(
        detailEnvelope(`/services/${params.id}/labels`, "ServiceLabels", {
          labels: service.Spec.Labels,
        }),
      );
    }),

    // Service spec field updates — each entry maps a sub-resource path to
    // a getter/setter pair on the service spec. All share the same handler
    // logic: look up the service, merge the request body, bump version, broadcast.
    ...(
      [
        [
          "resources",
          (service: Service) => service.Spec.TaskTemplate!.Resources ?? {},
          (service: Service, body: any) => {
            service.Spec.TaskTemplate!.Resources = {
              ...service.Spec.TaskTemplate!.Resources,
              ...body,
            };
          },
        ],
        [
          "ports",
          (service: Service) => service.Spec.EndpointSpec?.Ports ?? [],
          (service: Service, body: any) => {
            if (!service.Spec.EndpointSpec) service.Spec.EndpointSpec = {};
            service.Spec.EndpointSpec.Ports = body.ports ?? body;
          },
        ],
        [
          "update-policy",
          (service: Service) => service.Spec.UpdateConfig ?? {},
          (service: Service, body: any) => {
            service.Spec.UpdateConfig = { ...service.Spec.UpdateConfig, ...body };
          },
        ],
        [
          "rollback-policy",
          (service: Service) => service.Spec.RollbackConfig ?? {},
          (service: Service, body: any) => {
            service.Spec.RollbackConfig = { ...service.Spec.RollbackConfig, ...body };
          },
        ],
        [
          "log-driver",
          (service: Service) => service.Spec.TaskTemplate!.LogDriver ?? {},
          (service: Service, body: any) => {
            service.Spec.TaskTemplate!.LogDriver = {
              ...service.Spec.TaskTemplate!.LogDriver,
              ...body,
            };
          },
        ],
        [
          "healthcheck",
          (service: Service) => service.Spec.TaskTemplate!.ContainerSpec?.Healthcheck ?? null,
          (service: Service, body: any) => {
            if (service.Spec.TaskTemplate!.ContainerSpec)
              service.Spec.TaskTemplate!.ContainerSpec.Healthcheck = body;
          },
        ],
      ] as [string, (service: Service) => unknown, (service: Service, body: any) => void][]
    ).map(([field, getter, setter]) =>
      http.patch(`*/services/:id/${field}`, async ({ params, request }) => {
        const service = dataset.servicesByID.get(params.id as string);
        if (!service) return notFound();
        setter(service, await request.json());
        service.Version.Index++;
        service.UpdatedAt = new Date().toISOString();
        broadcastServiceUpdate(service);
        const responseKey = field.replace(/-([a-z])/g, (_, character: string) =>
          character.toUpperCase(),
        );
        return jsonResponse({ [responseKey]: getter(service) });
      }),
    ),

    http.put("*/services/:id/placement", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);
      if (!service) return notFound();
      service.Spec.TaskTemplate!.Placement = (await request.json()) as any;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse<{ placement: Placement | undefined }>({
        placement: service.Spec.TaskTemplate!.Placement,
      });
    }),

    // ---- Catch-all for HEAD requests (for Allow header checks) ----
    http.head("*", ({ request }) => {
      return new HttpResponse(null, {
        status: 200,
        headers: { Allow: allowFor(request.url), Link: editLinksFor(request.url) },
      });
    }),
  ];
}
