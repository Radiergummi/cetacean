import { randomHex, type Dataset } from "./dataset";
import { handleInstantQuery, handleRangeQuery } from "./prometheus";
import { broadcast, type SSEClients } from "./sseHandlers";
import type { ClusterSnapshot, HealthInfo, LogResponse } from "@/api/client";
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
  LogDriver,
  MonitoringStatus,
  Network,
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
  ServiceListItem,
  ServiceMount,
  ServiceNetworkRef,
  ServiceRef,
  ServiceSecretRef,
  Stack,
  StackDetail,
  StackSummary,
  SwarmInfo,
  Task,
  UpdateConfig,
  Volume,
  VolumeDetail,
} from "@/api/types";
import { http, HttpResponse, type JsonBodyType } from "msw";

const stackLabel = "com.docker.stack.namespace";

function jsonResponse<T extends JsonBodyType>(data: T, status = 200) {
  return HttpResponse.json(data, {
    status,
    headers: { Allow: "GET, HEAD, PUT, POST, PATCH, DELETE" },
  });
}

function detailEnvelope(id: string, type: string, extra: Record<string, unknown>) {
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
    Spec: { ContainerSpec: { Image: service.Spec.TaskTemplate.ContainerSpec?.Image ?? "" } },
    ServiceName: service.Spec.Name,
    NodeHostname: node.Description.Hostname,
  };
}

/**
 * Parse the Range header (e.g. "items 0-49") into offset and limit.
 */
function parseRange(request: Request): { offset: number; limit: number } {
  const range = request.headers.get("Range");

  if (range) {
    const match = range.match(/^items\s+(\d+)-(\d+)$/);

    if (match) {
      const start = parseInt(match[1], 10);
      const end = parseInt(match[2], 10);
      return { offset: start, limit: end - start + 1 };
    }
  }

  return { offset: 0, limit: 50 };
}

function paginate<T>(items: T[], request: Request): CollectionResponse<T> {
  const { offset, limit } = parseRange(request);
  const sliced = items.slice(offset, offset + limit);
  return { items: sliced, total: items.length, limit, offset };
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

function buildClusterSnapshot(dataset: Dataset): ClusterSnapshot {
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

  for (const node of dataset.nodes) {
    totalCPU += node.Description.Resources.NanoCPUs;
    totalMemory += node.Description.Resources.MemoryBytes;

    if (node.Status.State === "ready") {
      nodesReady++;
    } else {
      nodesDown++;
    }

    if (node.Spec.Availability === "drain") {
      nodesDraining++;
    }
  }

  let reservedCPU = 0;
  let reservedMemory = 0;

  for (const service of dataset.services) {
    const reservations = service.Spec.TaskTemplate.Resources?.Reservations;

    if (!reservations) {
      continue;
    }

    let replicas = 1;

    if (service.Spec.Mode.Replicated?.Replicas !== undefined) {
      replicas = service.Spec.Mode.Replicated.Replicas;
    } else if (service.Spec.Mode.Global) {
      replicas = dataset.nodes.length;
    }

    reservedCPU += (reservations.NanoCPUs ?? 0) * replicas;
    reservedMemory += (reservations.MemoryBytes ?? 0) * replicas;
  }

  let servicesConverged = 0;
  let servicesDegraded = 0;

  for (const service of dataset.services) {
    let desired = 0;

    if (service.Spec.Mode.Replicated?.Replicas !== undefined) {
      desired = service.Spec.Mode.Replicated.Replicas;
    } else if (service.Spec.Mode.Global) {
      desired = dataset.nodes.length;
    }

    const running = dataset.tasks.filter(
      (t) => t.ServiceID === service.ID && t.Status.State === "running",
    ).length;

    if (running >= desired) {
      servicesConverged++;
    } else {
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
    prometheusConfigured: true,
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

      const limits = service.Spec.TaskTemplate.Resources?.Limits;

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
        const image = service.Spec.TaskTemplate.ContainerSpec?.Image ?? "";
        return matches(service.Spec.Name) || matches(image);
      })
      .map((service) => ({
        id: service.ID,
        name: service.Spec.Name,
        detail: (service.Spec.TaskTemplate.ContainerSpec?.Image ?? "").split("@")[0],
      })),
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
      .filter((node) => matches(node.Description.Hostname))
      .map((node) => ({
        id: node.ID,
        name: node.Description.Hostname,
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

export function createHandlers(dataset: Dataset, clients: SSEClients) {
  function broadcastServiceUpdate(service: Service) {
    broadcast(clients, "service", "services", service.ID, {
      type: "service",
      action: "update",
      id: service.ID,
      resource: { ...service, RunningTasks: countRunningTasks(dataset, service.ID) },
    });
  }

  return [
    // ---- Health & meta ----
    http.get("*/-/health", () => {
      const data: HealthInfo = {
        status: "ok",
        version: "demo",
        commit: "demo",
        buildDate: new Date().toISOString(),
        operationsLevel: 3,
      };
      return jsonResponse<HealthInfo>(data);
    }),

    http.get("*/-/ready", () => {
      return jsonResponse<{ status: string }>({ status: "ready" });
    }),

    // The frontend calls /profile for whoami
    http.get("*/profile", () => {
      return jsonResponse(detailEnvelope("/profile", "Profile", {
        subject: "demo",
        displayName: "Demo User",
        provider: "none",
      }));
    }),

    http.get("*/auth/whoami", () => {
      return jsonResponse<Identity>({
        subject: "demo",
        displayName: "Demo User",
        provider: "none",
      });
    }),

    // ---- Cluster ----
    http.get("*/cluster/metrics", () => {
      return jsonResponse(detailEnvelope("/cluster/metrics", "ClusterMetrics", {
        cpu: { used: 4.2, total: 16, percent: 26.25 },
        memory: { used: 12_884_901_888, total: 34_359_738_368, percent: 37.5 },
        disk: { used: 53_687_091_200, total: 214_748_364_800, percent: 25.0 },
      }));
    }),

    http.get("*/cluster/capacity", () => {
      return jsonResponse<ClusterCapacity>({
        maxNodeCPU: 8_000_000_000,
        maxNodeMemory: 16 * 1024 * 1024 * 1024,
        totalCPU: 16_000_000_000,
        totalMemory: 32 * 1024 * 1024 * 1024,
        nodeCount: 3,
      });
    }),

    http.get("*/cluster", () => {
      return jsonResponse<ClusterSnapshot>(buildClusterSnapshot(dataset));
    }),

    // ---- Swarm ----
    http.get("*/swarm", () => {
      return jsonResponse<SwarmInfo>({
        swarm: dataset.swarm,
        managerAddr: "10.0.0.1:2377",
      });
    }),

    // ---- Topology (must be before */networks and */nodes to avoid glob conflicts) ----
    http.get("*/topology/networks", () => {
      const nodes = dataset.services.map((service) => ({
        id: service.ID,
        name: service.Spec.Name,
        stack: service.Spec.Labels?.[stackLabel],
        replicas:
          service.Spec.Mode.Replicated?.Replicas ??
          (service.Spec.Mode.Global ? dataset.nodes.length : 1),
        image: (service.Spec.TaskTemplate.ContainerSpec?.Image ?? "").split("@")[0],
        ports: (service.Spec.EndpointSpec?.Ports ?? []).map(
          ({ PublishedPort, TargetPort, Protocol }) => `${PublishedPort}:${TargetPort}/${Protocol}`,
        ),
        mode: service.Spec.Mode.Replicated ? "replicated" : "global",
        updateStatus: service.UpdateStatus?.State,
        networkAliases: {},
      }));

      const edges: { source: string; target: string; networks: string[] }[] = [];
      const serviceNetworks = new Map<string, string[]>();

      for (const service of dataset.services) {
        const targets = (service.Spec.TaskTemplate.Networks ?? []).map(({ Target }) => Target);
        serviceNetworks.set(service.ID, targets);
      }

      const seen = new Set<string>();

      for (const [serviceA, networksA] of serviceNetworks) {
        for (const [serviceB, networksB] of serviceNetworks) {
          if (serviceA >= serviceB) continue;

          const shared = networksA.filter((id) => networksB.includes(id));
          const key = `${serviceA}-${serviceB}`;

          if (shared.length > 0 && !seen.has(key)) {
            seen.add(key);
            edges.push({ source: serviceA, target: serviceB, networks: shared });
          }
        }
      }

      const networks = dataset.networks.map((network) => ({
        id: network.Id,
        name: network.Name,
        driver: network.Driver,
        scope: network.Scope,
        stack: network.Labels?.[stackLabel],
      }));

      return jsonResponse({ nodes, edges, networks });
    }),

    http.get("*/topology/placement", () => {
      const placementNodes = dataset.nodes.map((node) => ({
        id: node.ID,
        hostname: node.Description.Hostname,
        role: node.Spec.Role,
        state: node.Status.State,
        availability: node.Spec.Availability,
        tasks: dataset.tasks
          .filter((task) => task.NodeID === node.ID && task.Status.State === "running")
          .map((task) => {
            const service = dataset.servicesByID.get(task.ServiceID);

            return {
              id: task.ID,
              serviceId: task.ServiceID,
              serviceName: service?.Spec.Name ?? "",
              state: task.Status.State,
              slot: task.Slot ?? 0,
              image: (task.Spec.ContainerSpec?.Image ?? "").split("@")[0],
            };
          }),
      }));

      return jsonResponse({ nodes: placementNodes });
    }),

    // ---- Nodes ----
    http.get("*/nodes/:id/tasks", ({ params, request }) => {
      const nodeID = params.id as string;
      const nodeTasks = dataset.tasks.filter((task) => task.NodeID === nodeID);
      return jsonResponse<CollectionResponse<Task>>(paginate(nodeTasks, request));
    }),

    http.get("*/nodes/:id/labels", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ labels: Record<string, string> }>({ labels: node.Spec.Labels ?? {} });
    }),

    http.get("*/nodes/:id/role", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const managerCount = dataset.nodes.filter((node) => node.Spec.Role === "manager").length;
      return jsonResponse<{ role: string; isLeader: boolean; managerCount: number }>({
        role: node.Spec.Role,
        isLeader: node.ManagerStatus?.Leader ?? false,
        managerCount,
      });
    }),

    http.get("*/nodes/:id", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ node: Node }>({ node });
    }),

    http.get("*/nodes", ({ request }) => {
      return jsonResponse<CollectionResponse<Node>>(paginate(dataset.nodes, request));
    }),

    // ---- Services ----
    http.get("*/services/:id/tasks", ({ params, request }) => {
      const serviceID = params.id as string;
      const serviceTasks = dataset.tasks.filter((task) => task.ServiceID === serviceID);
      return jsonResponse<CollectionResponse<Task>>(paginate(serviceTasks, request));
    }),

    http.get("*/services/:id/env", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const envArray = service.Spec.TaskTemplate.ContainerSpec?.Env ?? [];
      const env: Record<string, string> = {};

      for (const entry of envArray) {
        const equalsIndex = entry.indexOf("=");

        if (equalsIndex >= 0) {
          env[entry.slice(0, equalsIndex)] = entry.slice(equalsIndex + 1);
        }
      }

      return jsonResponse<{ env: Record<string, string> }>({ env });
    }),

    http.get("*/services/:id/labels", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ labels: Record<string, string> }>({
        labels: service.Spec.Labels ?? {},
      });
    }),

    http.get("*/services/:id/resources", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ resources: Record<string, unknown> }>({
        resources: service.Spec.TaskTemplate.Resources ?? {},
      });
    }),

    http.get("*/services/:id/healthcheck", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ healthcheck: Healthcheck | null }>({
        healthcheck: service.Spec.TaskTemplate.ContainerSpec?.Healthcheck ?? null,
      });
    }),

    http.get("*/services/:id/configs", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const configs = (service.Spec.TaskTemplate.ContainerSpec?.Configs ?? []).map(
        ({ ConfigID, ConfigName, File }) => ({
          configID: ConfigID,
          configName: ConfigName,
          fileName: File?.Name ?? "",
        }),
      );
      return jsonResponse<{ configs: ServiceConfigRef[] }>({ configs });
    }),

    http.get("*/services/:id/secrets", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const secrets = (service.Spec.TaskTemplate.ContainerSpec?.Secrets ?? []).map(
        ({ SecretID, SecretName, File }) => ({
          secretID: SecretID,
          secretName: SecretName,
          fileName: File?.Name ?? "",
        }),
      );
      return jsonResponse<{ secrets: ServiceSecretRef[] }>({ secrets });
    }),

    http.get("*/services/:id/networks", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const networks = (service.Spec.TaskTemplate.Networks ?? []).map(({ Target, Aliases }) => ({
        target: Target,
        aliases: Aliases ?? undefined,
      }));
      return jsonResponse<{ networks: ServiceNetworkRef[] }>({ networks });
    }),

    http.get("*/services/:id/mounts", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ mounts: ServiceMount[] }>({
        mounts: service.Spec.TaskTemplate.ContainerSpec?.Mounts ?? [],
      });
    }),

    http.get("*/services/:id/ports", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ ports: PortConfig[] }>({
        ports: service.Spec.EndpointSpec?.Ports ?? [],
      });
    }),

    http.get("*/services/:id/placement", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ placement: Placement }>({
        placement: service.Spec.TaskTemplate.Placement ?? {},
      });
    }),

    http.get("*/services/:id/update-policy", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ updatePolicy: Partial<UpdateConfig> }>({
        updatePolicy: service.Spec.UpdateConfig ?? {},
      });
    }),

    http.get("*/services/:id/rollback-policy", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ rollbackPolicy: Partial<UpdateConfig> }>({
        rollbackPolicy: service.Spec.RollbackConfig ?? {},
      });
    }),

    http.get("*/services/:id/log-driver", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ logDriver: Partial<LogDriver> }>({
        logDriver: service.Spec.TaskTemplate.LogDriver ?? {},
      });
    }),

    http.get("*/services/:id/container-config", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const container = service.Spec.TaskTemplate.ContainerSpec;
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

    http.get("*/services/:id/logs", () => {
      return jsonResponse<LogResponse>({ lines: [], oldest: "", newest: "", hasMore: false });
    }),

    http.get("*/services/:id", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<ServiceDetail>({ service, changes: [], integrations: [] });
    }),

    http.get("*/services", ({ request }) => {
      const items = dataset.services.map((service) => ({
        ...service,
        RunningTasks: countRunningTasks(dataset, service.ID),
      }));
      return jsonResponse<CollectionResponse<ServiceListItem>>(paginate(items, request));
    }),

    // ---- Tasks ----
    http.get("*/tasks/:id/logs", () => {
      return jsonResponse<LogResponse>({ lines: [], oldest: "", newest: "", hasMore: false });
    }),

    http.get("*/tasks/:id", ({ params }) => {
      const task = dataset.tasksByID.get(params.id as string);

      if (!task) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ task: Task }>({ task });
    }),

    http.get("*/tasks", ({ request }) => {
      return jsonResponse<CollectionResponse<Task>>(paginate(dataset.tasks, request));
    }),

    // ---- Stacks ----
    http.get("*/stacks/summary", () => {
      const items = buildStackSummaries(dataset);
      return jsonResponse<CollectionResponse<StackSummary>>({
        items,
        total: items.length,
        limit: 50,
        offset: 0,
      });
    }),

    http.get("*/stacks/:name", ({ params }) => {
      const stackName = params.name as string;
      const stacks = deriveStacks(dataset);
      const stack = stacks.get(stackName);

      if (!stack) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<{ stack: StackDetail }>({
        stack: {
          name: stackName,
          services: stack.services
            .map((id) => dataset.servicesByID.get(id))
            .filter(Boolean) as Service[],
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
      });
    }),

    http.get("*/stacks", ({ request }) => {
      const stacks = deriveStacks(dataset);
      const items = Array.from(stacks.entries()).map(([name, stack]) => ({
        name,
        services: stack.services,
        configs: stack.configs,
        secrets: stack.secrets,
        networks: stack.networks,
        volumes: stack.volumes,
      }));
      return jsonResponse<CollectionResponse<Stack>>(paginate(items, request));
    }),

    // ---- Configs ----
    http.get("*/configs/:id", ({ params }) => {
      const config = dataset.configsByID.get(params.id as string);

      if (!config) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<ConfigDetail>({
        config,
        services: findServicesUsing(
          dataset,
          (service) =>
            service.Spec.TaskTemplate.ContainerSpec?.Configs?.some(
              ({ ConfigID }) => ConfigID === config.ID,
            ) ?? false,
        ),
      });
    }),

    http.get("*/configs", ({ request }) => {
      return jsonResponse<CollectionResponse<Config>>(paginate(dataset.configs, request));
    }),

    // ---- Secrets ----
    http.get("*/secrets/:id", ({ params }) => {
      const secret = dataset.secretsByID.get(params.id as string);

      if (!secret) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<SecretDetail>({
        secret,
        services: findServicesUsing(
          dataset,
          (service) =>
            service.Spec.TaskTemplate.ContainerSpec?.Secrets?.some(
              ({ SecretID }) => SecretID === secret.ID,
            ) ?? false,
        ),
      });
    }),

    http.get("*/secrets", ({ request }) => {
      return jsonResponse<CollectionResponse<Secret>>(paginate(dataset.secrets, request));
    }),

    // ---- Networks ----
    http.get("*/networks/:id", ({ params }) => {
      const network = dataset.networksByID.get(params.id as string);

      if (!network) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<NetworkDetail>({
        network,
        services: findServicesUsing(
          dataset,
          (service) =>
            service.Spec.TaskTemplate.Networks?.some(({ Target }) => Target === network.Id) ??
            false,
        ),
      });
    }),

    http.get("*/networks", ({ request }) => {
      return jsonResponse<CollectionResponse<Network>>(paginate(dataset.networks, request));
    }),

    // ---- Volumes ----
    http.get("*/volumes/:name", ({ params }) => {
      const volume = dataset.volumesByName.get(params.name as string);

      if (!volume) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse<VolumeDetail>({
        volume,
        services: findServicesUsing(
          dataset,
          (service) =>
            service.Spec.TaskTemplate.ContainerSpec?.Mounts?.some(
              ({ Source }) => Source === volume.Name,
            ) ?? false,
        ),
      });
    }),

    http.get("*/volumes", ({ request }) => {
      return jsonResponse<CollectionResponse<Volume>>(paginate(dataset.volumes, request));
    }),

    // ---- Search ----
    http.get("*/search", ({ request }) => {
      const url = new URL(request.url);
      const query = url.searchParams.get("q") ?? "";
      const limitParam = url.searchParams.get("limit");
      const limit = limitParam !== null ? parseInt(limitParam, 10) : 3;

      const { results, counts, total } = searchDataset(dataset, query, limit);
      return jsonResponse<SearchResponse>({ query, results, counts, total });
    }),

    // ---- History ----
    http.get("*/history", () => {
      const items: HistoryEntry[] = [];
      return jsonResponse<CollectionResponse<HistoryEntry>>({
        items,
        total: 0,
        limit: 50,
        offset: 0,
      });
    }),

    // ---- Recommendations ----
    http.get("*/recommendations", () => {
      return jsonResponse<RecommendationsResponse>({
        items: [],
        total: 0,
        summary: { critical: 0, warning: 0, info: 0 },
        computedAt: new Date().toISOString(),
      });
    }),

    // ---- Disk usage ----
    http.get("*/disk-usage", () => {
      return jsonResponse<CollectionResponse<DiskUsageSummary>>({
        items: [
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
        ],
        total: 4,
        limit: 50,
        offset: 0,
      });
    }),

    // ---- Plugins ----
    http.get("*/plugins", () => {
      return jsonResponse<CollectionResponse<Plugin>>({
        items: [],
        total: 0,
        limit: 50,
        offset: 0,
      });
    }),

    // ---- Monitoring status ----
    http.get("*/metrics/status", () => {
      return jsonResponse<MonitoringStatus>({
        prometheusConfigured: true,
        prometheusReachable: true,
        nodeExporter: { targets: 3, nodes: 3 },
        cadvisor: { targets: 3, nodes: 3 },
      });
    }),

    // ---- Metrics (Prometheus proxy) ----
    http.get("*/metrics/labels/:name", ({ params }) => {
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
        nodename: dataset.nodes.map((node) => node.Description.Hostname),
      };
      return jsonResponse<{ data: string[] }>({ data: valueMap[name] ?? [] });
    }),

    http.get("*/metrics/labels", () => {
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

    http.get("*/metrics", ({ request }) => {
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
    http.get("*/-/docker-latest-version", () => {
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

      if (service.Spec.TaskTemplate.ContainerSpec) {
        service.Spec.TaskTemplate.ContainerSpec.Image = body.image;
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

      dataset.services = dataset.services.filter((s) => s.ID !== id);
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
      return jsonResponse(detailEnvelope(`/nodes/${params.id}/labels`, "NodeLabels", {
        labels: node.Spec.Labels,
      }));
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
      const container = service.Spec.TaskTemplate.ContainerSpec;

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
      return jsonResponse(detailEnvelope(`/services/${params.id}/env`, "ServiceEnv", {
        env: Object.fromEntries(envMap),
      }));
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
      return jsonResponse(detailEnvelope(`/services/${params.id}/labels`, "ServiceLabels", {
        labels: service.Spec.Labels,
      }));
    }),

    // Service spec field updates — each entry maps a sub-resource path to
    // a getter/setter pair on the service spec. All share the same handler
    // logic: look up the service, merge the request body, bump version, broadcast.
    ...(
      [
        [
          "resources",
          (service: Service) => service.Spec.TaskTemplate.Resources ?? {},
          (service: Service, body: any) => {
            service.Spec.TaskTemplate.Resources = {
              ...service.Spec.TaskTemplate.Resources,
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
          (service: Service) => service.Spec.TaskTemplate.LogDriver ?? {},
          (service: Service, body: any) => {
            service.Spec.TaskTemplate.LogDriver = {
              ...service.Spec.TaskTemplate.LogDriver,
              ...body,
            };
          },
        ],
        [
          "healthcheck",
          (service: Service) => service.Spec.TaskTemplate.ContainerSpec?.Healthcheck ?? null,
          (service: Service, body: any) => {
            if (service.Spec.TaskTemplate.ContainerSpec)
              service.Spec.TaskTemplate.ContainerSpec.Healthcheck = body;
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
      service.Spec.TaskTemplate.Placement = (await request.json()) as any;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse<{ placement: Placement | undefined }>({
        placement: service.Spec.TaskTemplate.Placement,
      });
    }),

    // ---- Catch-all for HEAD requests (for Allow header checks) ----
    http.head("*", () => {
      return new HttpResponse(null, {
        status: 200,
        headers: { Allow: "GET, HEAD, PUT, POST, PATCH, DELETE" },
      });
    }),
  ];
}
