import type { Dataset } from "./dataset";
import { handleInstantQuery, handleRangeQuery } from "./prometheus";
import { broadcast, type SSEClients } from "./sseHandlers";
import type { ClusterSnapshot, ClusterMetrics, HealthInfo } from "@/api/client";
import type {
  CollectionResponse,
  HistoryEntry,
  Node,
  SearchResourceType,
  SearchResult,
  Service,
  ServiceRef,
  StackSummary,
  Task,
} from "@/api/types";
import { http, HttpResponse } from "msw";

const stackLabel = "com.docker.stack.namespace";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function jsonResponse(data: any, status = 200) {
  return HttpResponse.json(data, {
    status,
    headers: { Allow: "GET, HEAD, PUT, POST, PATCH, DELETE" },
  });
}

function notFound() {
  return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
}

let taskIdCounter = 2000;

function createTask(service: Service, slot: number, node: Node): Task {
  taskIdCounter++;
  return {
    ID: `tk${String(taskIdCounter).padStart(23, "0")}`,
    Version: { Index: 300 + taskIdCounter },
    ServiceID: service.ID,
    NodeID: node.ID,
    Slot: slot,
    Status: {
      Timestamp: new Date().toISOString(),
      State: "running",
      Message: "started",
      ContainerStatus: {
        ContainerID: crypto.randomUUID().replace(/-/g, "").padEnd(64, "0"),
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

function findServicesUsingConfig(dataset: Dataset, configID: string): ServiceRef[] {
  const refs: ServiceRef[] = [];

  for (const service of dataset.services) {
    const configs = service.Spec.TaskTemplate.ContainerSpec?.Configs;

    if (configs?.some((c) => c.ConfigID === configID)) {
      refs.push({ id: service.ID, name: service.Spec.Name });
    }
  }

  return refs;
}

function findServicesUsingSecret(dataset: Dataset, secretID: string): ServiceRef[] {
  const refs: ServiceRef[] = [];

  for (const service of dataset.services) {
    const secrets = service.Spec.TaskTemplate.ContainerSpec?.Secrets;

    if (secrets?.some((s) => s.SecretID === secretID)) {
      refs.push({ id: service.ID, name: service.Spec.Name });
    }
  }

  return refs;
}

function findServicesUsingNetwork(dataset: Dataset, networkID: string): ServiceRef[] {
  const refs: ServiceRef[] = [];

  for (const service of dataset.services) {
    const networks = service.Spec.TaskTemplate.Networks;

    if (networks?.some((n) => n.Target === networkID)) {
      refs.push({ id: service.ID, name: service.Spec.Name });
    }
  }

  return refs;
}

function findServicesUsingVolume(dataset: Dataset, volumeName: string): ServiceRef[] {
  const refs: ServiceRef[] = [];

  for (const service of dataset.services) {
    const mounts = service.Spec.TaskTemplate.ContainerSpec?.Mounts;

    if (mounts?.some((m) => m.Source === volumeName)) {
      refs.push({ id: service.ID, name: service.Spec.Name });
    }
  }

  return refs;
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

  const match = (name: string): boolean => name.toLowerCase().includes(lowerQuery);

  const results: Partial<Record<SearchResourceType, SearchResult[]>> = {};
  const counts: Partial<Record<SearchResourceType, number>> = {};
  let total = 0;

  // Services
  const serviceResults: SearchResult[] = [];

  for (const service of dataset.services) {
    const image = service.Spec.TaskTemplate.ContainerSpec?.Image ?? "";

    if (match(service.Spec.Name) || match(image)) {
      serviceResults.push({
        id: service.ID,
        name: service.Spec.Name,
        detail: image.split("@")[0],
      });
    }
  }

  if (serviceResults.length > 0) {
    results.services = limit > 0 ? serviceResults.slice(0, limit) : serviceResults;
    counts.services = serviceResults.length;
    total += serviceResults.length;
  }

  // Stacks
  const stacks = deriveStacks(dataset);
  const stackResults: SearchResult[] = [];

  for (const [name, stack] of stacks) {
    if (match(name)) {
      stackResults.push({
        id: name,
        name,
        detail: `${stack.services.length} services`,
      });
    }
  }

  if (stackResults.length > 0) {
    results.stacks = limit > 0 ? stackResults.slice(0, limit) : stackResults;
    counts.stacks = stackResults.length;
    total += stackResults.length;
  }

  // Nodes
  const nodeResults: SearchResult[] = [];

  for (const node of dataset.nodes) {
    if (match(node.Description.Hostname)) {
      nodeResults.push({
        id: node.ID,
        name: node.Description.Hostname,
        detail: `${node.Spec.Role} (${node.Status.State})`,
      });
    }
  }

  if (nodeResults.length > 0) {
    results.nodes = limit > 0 ? nodeResults.slice(0, limit) : nodeResults;
    counts.nodes = nodeResults.length;
    total += nodeResults.length;
  }

  // Configs
  const configResults: SearchResult[] = [];

  for (const config of dataset.configs) {
    if (match(config.Spec.Name)) {
      configResults.push({
        id: config.ID,
        name: config.Spec.Name,
        detail: "",
      });
    }
  }

  if (configResults.length > 0) {
    results.configs = limit > 0 ? configResults.slice(0, limit) : configResults;
    counts.configs = configResults.length;
    total += configResults.length;
  }

  // Secrets
  const secretResults: SearchResult[] = [];

  for (const secret of dataset.secrets) {
    if (match(secret.Spec.Name)) {
      secretResults.push({
        id: secret.ID,
        name: secret.Spec.Name,
        detail: "",
      });
    }
  }

  if (secretResults.length > 0) {
    results.secrets = limit > 0 ? secretResults.slice(0, limit) : secretResults;
    counts.secrets = secretResults.length;
    total += secretResults.length;
  }

  // Networks
  const networkResults: SearchResult[] = [];

  for (const network of dataset.networks) {
    if (match(network.Name)) {
      networkResults.push({
        id: network.Id,
        name: network.Name,
        detail: `${network.Driver} (${network.Scope})`,
      });
    }
  }

  if (networkResults.length > 0) {
    results.networks = limit > 0 ? networkResults.slice(0, limit) : networkResults;
    counts.networks = networkResults.length;
    total += networkResults.length;
  }

  // Volumes
  const volumeResults: SearchResult[] = [];

  for (const volume of dataset.volumes) {
    if (match(volume.Name)) {
      volumeResults.push({
        id: volume.Name,
        name: volume.Name,
        detail: `${volume.Driver} (${volume.Scope})`,
      });
    }
  }

  if (volumeResults.length > 0) {
    results.volumes = limit > 0 ? volumeResults.slice(0, limit) : volumeResults;
    counts.volumes = volumeResults.length;
    total += volumeResults.length;
  }

  return { results, counts, total };
}

function countRunningTasks(dataset: Dataset, serviceID: string): number {
  return dataset.tasks.filter((t) => t.ServiceID === serviceID && t.Status.State === "running")
    .length;
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
      return jsonResponse(data);
    }),

    http.get("*/-/ready", () => {
      return jsonResponse({ status: "ready" });
    }),

    // The frontend calls /profile for whoami
    http.get("*/profile", () => {
      return jsonResponse({ subject: "demo", displayName: "Demo User", provider: "none" });
    }),

    http.get("*/auth/whoami", () => {
      return jsonResponse({ subject: "demo", displayName: "Demo User", provider: "none" });
    }),

    // ---- Cluster ----
    http.get("*/cluster/metrics", () => {
      const data: ClusterMetrics = {
        cpu: { used: 4.2, total: 16, percent: 26.25 },
        memory: { used: 12_884_901_888, total: 34_359_738_368, percent: 37.5 },
        disk: { used: 53_687_091_200, total: 214_748_364_800, percent: 25.0 },
      };
      return jsonResponse(data);
    }),

    http.get("*/cluster/capacity", () => {
      return jsonResponse({
        maxNodeCPU: 8_000_000_000,
        maxNodeMemory: 16 * 1024 * 1024 * 1024,
        totalCPU: 16_000_000_000,
        totalMemory: 32 * 1024 * 1024 * 1024,
        nodeCount: 3,
      });
    }),

    http.get("*/cluster", () => {
      return jsonResponse(buildClusterSnapshot(dataset));
    }),

    // ---- Swarm ----
    http.get("*/swarm", () => {
      return jsonResponse({
        swarm: dataset.swarm,
        managerAddr: "10.0.0.1:2377",
      });
    }),

    // ---- Topology (must be before */networks and */nodes to avoid glob conflicts) ----
    http.get("*/topology/networks", () => {
      const sLabel = "com.docker.stack.namespace";

      const nodes = dataset.services.map((svc) => ({
        id: svc.ID,
        name: svc.Spec.Name,
        stack: svc.Spec.Labels?.[sLabel],
        replicas:
          svc.Spec.Mode.Replicated?.Replicas ?? (svc.Spec.Mode.Global ? dataset.nodes.length : 1),
        image: (svc.Spec.TaskTemplate.ContainerSpec?.Image ?? "").split("@")[0],
        ports: (svc.Spec.EndpointSpec?.Ports ?? []).map(
          (p) => `${p.PublishedPort}:${p.TargetPort}/${p.Protocol}`,
        ),
        mode: svc.Spec.Mode.Replicated ? "replicated" : "global",
        updateStatus: svc.UpdateStatus?.State,
        networkAliases: {},
      }));

      const edges: { source: string; target: string; networks: string[] }[] = [];
      const serviceNetworks = new Map<string, string[]>();

      for (const svc of dataset.services) {
        const nets = (svc.Spec.TaskTemplate.Networks ?? []).map((n) => n.Target);
        serviceNetworks.set(svc.ID, nets);
      }

      const seen = new Set<string>();

      for (const [svcA, netsA] of serviceNetworks) {
        for (const [svcB, netsB] of serviceNetworks) {
          if (svcA >= svcB) continue;
          const shared = netsA.filter((n) => netsB.includes(n));

          if (shared.length > 0) {
            const key = `${svcA}-${svcB}`;

            if (!seen.has(key)) {
              seen.add(key);
              edges.push({ source: svcA, target: svcB, networks: shared });
            }
          }
        }
      }

      const networks = dataset.networks.map((net) => ({
        id: net.Id,
        name: net.Name,
        driver: net.Driver,
        scope: net.Scope,
        stack: net.Labels?.[sLabel],
      }));

      return jsonResponse({ nodes, edges, networks });
    }),

    http.get("*/topology/placement", () => {
      const topoNodes = dataset.nodes.map((node) => ({
        id: node.ID,
        hostname: node.Description.Hostname,
        role: node.Spec.Role,
        state: node.Status.State,
        availability: node.Spec.Availability,
        tasks: dataset.tasks
          .filter((t) => t.NodeID === node.ID && t.Status.State === "running")
          .map((t) => {
            const svc = dataset.servicesByID.get(t.ServiceID);

            return {
              id: t.ID,
              serviceId: t.ServiceID,
              serviceName: svc?.Spec.Name ?? "",
              state: t.Status.State,
              slot: t.Slot ?? 0,
              image: (t.Spec.ContainerSpec?.Image ?? "").split("@")[0],
            };
          }),
      }));

      return jsonResponse({ nodes: topoNodes });
    }),

    // ---- Nodes ----
    http.get("*/nodes/:id/tasks", ({ params, request }) => {
      const nodeID = params.id as string;
      const nodeTasks = dataset.tasks.filter((t) => t.NodeID === nodeID);
      return jsonResponse(paginate(nodeTasks, request));
    }),

    http.get("*/nodes/:id/labels", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ labels: node.Spec.Labels ?? {} });
    }),

    http.get("*/nodes/:id/role", ({ params }) => {
      const node = dataset.nodesByID.get(params.id as string);

      if (!node) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const managerCount = dataset.nodes.filter((n) => n.Spec.Role === "manager").length;
      return jsonResponse({
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

      return jsonResponse({ node });
    }),

    http.get("*/nodes", ({ request }) => {
      return jsonResponse(paginate(dataset.nodes, request));
    }),

    // ---- Services ----
    http.get("*/services/:id/tasks", ({ params, request }) => {
      const serviceID = params.id as string;
      const serviceTasks = dataset.tasks.filter((t) => t.ServiceID === serviceID);
      return jsonResponse(paginate(serviceTasks, request));
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

      return jsonResponse({ env });
    }),

    http.get("*/services/:id/labels", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ labels: service.Spec.Labels ?? {} });
    }),

    http.get("*/services/:id/resources", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ resources: service.Spec.TaskTemplate.Resources ?? {} });
    }),

    http.get("*/services/:id/healthcheck", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({
        healthcheck: service.Spec.TaskTemplate.ContainerSpec?.Healthcheck ?? null,
      });
    }),

    http.get("*/services/:id/configs", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const configs = (service.Spec.TaskTemplate.ContainerSpec?.Configs ?? []).map((c) => ({
        configID: c.ConfigID,
        configName: c.ConfigName,
        fileName: c.File?.Name ?? "",
      }));
      return jsonResponse({ configs });
    }),

    http.get("*/services/:id/secrets", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const secrets = (service.Spec.TaskTemplate.ContainerSpec?.Secrets ?? []).map((s) => ({
        secretID: s.SecretID,
        secretName: s.SecretName,
        fileName: s.File?.Name ?? "",
      }));
      return jsonResponse({ secrets });
    }),

    http.get("*/services/:id/networks", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const networks = (service.Spec.TaskTemplate.Networks ?? []).map((n) => ({
        target: n.Target,
        aliases: n.Aliases,
      }));
      return jsonResponse({ networks });
    }),

    http.get("*/services/:id/mounts", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ mounts: service.Spec.TaskTemplate.ContainerSpec?.Mounts ?? [] });
    }),

    http.get("*/services/:id/ports", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ ports: service.Spec.EndpointSpec?.Ports ?? [] });
    }),

    http.get("*/services/:id/placement", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ placement: service.Spec.TaskTemplate.Placement ?? {} });
    }),

    http.get("*/services/:id/update-policy", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ updatePolicy: service.Spec.UpdateConfig ?? {} });
    }),

    http.get("*/services/:id/rollback-policy", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ rollbackPolicy: service.Spec.RollbackConfig ?? {} });
    }),

    http.get("*/services/:id/log-driver", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ logDriver: service.Spec.TaskTemplate.LogDriver ?? {} });
    }),

    http.get("*/services/:id/container-config", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      const container = service.Spec.TaskTemplate.ContainerSpec;
      return jsonResponse({
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
        dnsConfig: container?.DNSConfig ?? {},
      });
    }),

    http.get("*/services/:id/logs", () => {
      return jsonResponse({ lines: [], oldest: "", newest: "", hasMore: false });
    }),

    http.get("*/services/:id", ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ service, changes: [], integrations: [] });
    }),

    http.get("*/services", ({ request }) => {
      const items = dataset.services.map((service) => ({
        ...service,
        RunningTasks: countRunningTasks(dataset, service.ID),
      }));
      return jsonResponse(paginate(items, request));
    }),

    // ---- Tasks ----
    http.get("*/tasks/:id/logs", () => {
      return jsonResponse({ lines: [], oldest: "", newest: "", hasMore: false });
    }),

    http.get("*/tasks/:id", ({ params }) => {
      const task = dataset.tasksByID.get(params.id as string);

      if (!task) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({ task });
    }),

    http.get("*/tasks", ({ request }) => {
      return jsonResponse(paginate(dataset.tasks, request));
    }),

    // ---- Stacks ----
    http.get("*/stacks/summary", () => {
      const items = buildStackSummaries(dataset);
      return jsonResponse({ items, total: items.length, limit: 50, offset: 0 });
    }),

    http.get("*/stacks/:name", ({ params }) => {
      const stackName = params.name as string;
      const stacks = deriveStacks(dataset);
      const stack = stacks.get(stackName);

      if (!stack) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({
        stack: {
          name: stackName,
          services: stack.services.map((id) => dataset.servicesByID.get(id)).filter(Boolean),
          configs: stack.configs.map((id) => dataset.configsByID.get(id)).filter(Boolean),
          secrets: stack.secrets.map((id) => dataset.secretsByID.get(id)).filter(Boolean),
          networks: stack.networks.map((id) => dataset.networksByID.get(id)).filter(Boolean),
          volumes: stack.volumes.map((name) => dataset.volumesByName.get(name)).filter(Boolean),
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
      return jsonResponse(paginate(items, request));
    }),

    // ---- Configs ----
    http.get("*/configs/:id", ({ params }) => {
      const config = dataset.configsByID.get(params.id as string);

      if (!config) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({
        config,
        services: findServicesUsingConfig(dataset, config.ID),
      });
    }),

    http.get("*/configs", ({ request }) => {
      return jsonResponse(paginate(dataset.configs, request));
    }),

    // ---- Secrets ----
    http.get("*/secrets/:id", ({ params }) => {
      const secret = dataset.secretsByID.get(params.id as string);

      if (!secret) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({
        secret,
        services: findServicesUsingSecret(dataset, secret.ID),
      });
    }),

    http.get("*/secrets", ({ request }) => {
      return jsonResponse(paginate(dataset.secrets, request));
    }),

    // ---- Networks ----
    http.get("*/networks/:id", ({ params }) => {
      const network = dataset.networksByID.get(params.id as string);

      if (!network) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({
        network,
        services: findServicesUsingNetwork(dataset, network.Id),
      });
    }),

    http.get("*/networks", ({ request }) => {
      return jsonResponse(paginate(dataset.networks, request));
    }),

    // ---- Volumes ----
    http.get("*/volumes/:name", ({ params }) => {
      const volume = dataset.volumesByName.get(params.name as string);

      if (!volume) {
        return HttpResponse.json({ title: "Not Found", status: 404 }, { status: 404 });
      }

      return jsonResponse({
        volume,
        services: findServicesUsingVolume(dataset, volume.Name),
      });
    }),

    http.get("*/volumes", ({ request }) => {
      return jsonResponse(paginate(dataset.volumes, request));
    }),

    // ---- Search ----
    http.get("*/search", ({ request }) => {
      const url = new URL(request.url);
      const query = url.searchParams.get("q") ?? "";
      const limitParam = url.searchParams.get("limit");
      const limit = limitParam !== null ? parseInt(limitParam, 10) : 3;

      const { results, counts, total } = searchDataset(dataset, query, limit);
      return jsonResponse({ query, results, counts, total });
    }),

    // ---- History ----
    http.get("*/history", () => {
      const items: HistoryEntry[] = [];
      return jsonResponse({ items, total: 0, limit: 50, offset: 0 });
    }),

    // ---- Recommendations ----
    http.get("*/recommendations", () => {
      return jsonResponse({
        items: [],
        total: 0,
        summary: { critical: 0, warning: 0, info: 0 },
        computedAt: new Date().toISOString(),
      });
    }),

    // ---- Disk usage ----
    http.get("*/disk-usage", () => {
      return jsonResponse({
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
      return jsonResponse({ items: [], total: 0, limit: 50, offset: 0 });
    }),

    // ---- Monitoring status ----
    http.get("*/metrics/status", () => {
      return jsonResponse({
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
        name: dataset.services.map((s) => s.Spec.Name),
        instance: dataset.nodes.map((n) => `${n.Description.Hostname}:9100`),
        job: ["cadvisor", "node-exporter", "prometheus"],
        nodename: dataset.nodes.map((n) => n.Description.Hostname),
      };
      return jsonResponse({ data: valueMap[name] ?? [] });
    }),

    http.get("*/metrics/labels", () => {
      return jsonResponse({
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
        return jsonResponse({ data });
      }

      const data = handleInstantQuery(query, dataset);
      return jsonResponse({ data });
    }),

    // ---- Docker latest version ----
    http.get("*/-/docker-latest-version", () => {
      return jsonResponse({
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
          const workers = dataset.nodes.filter((n) => n.Spec.Role === "worker");
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
      return jsonResponse(service);
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
      return jsonResponse(service);
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
      return jsonResponse(service);
    }),

    http.post("*/services/:id/restart", async ({ params }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse(service);
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
      return jsonResponse(node);
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
      return jsonResponse({ labels: node.Spec.Labels });
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
      return jsonResponse({ env: Object.fromEntries(envMap) });
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
      return jsonResponse({ labels: service.Spec.Labels });
    }),

    http.patch("*/services/:id/resources", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as Record<string, unknown>;
      service.Spec.TaskTemplate.Resources = {
        ...service.Spec.TaskTemplate.Resources,
        ...body,
      };
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse({ resources: service.Spec.TaskTemplate.Resources });
    }),

    http.patch("*/services/:id/ports", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as { ports: unknown[] };

      if (!service.Spec.EndpointSpec) {
        service.Spec.EndpointSpec = {};
      }

      service.Spec.EndpointSpec.Ports = body.ports as any;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse({ ports: service.Spec.EndpointSpec.Ports });
    }),

    http.put("*/services/:id/placement", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = await request.json();
      service.Spec.TaskTemplate.Placement = body as any;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse({ placement: service.Spec.TaskTemplate.Placement });
    }),

    http.patch("*/services/:id/update-policy", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as Record<string, unknown>;
      service.Spec.UpdateConfig = { ...service.Spec.UpdateConfig, ...body } as any;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse({ updatePolicy: service.Spec.UpdateConfig });
    }),

    http.patch("*/services/:id/rollback-policy", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as Record<string, unknown>;
      service.Spec.RollbackConfig = { ...service.Spec.RollbackConfig, ...body } as any;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse({ rollbackPolicy: service.Spec.RollbackConfig });
    }),

    http.patch("*/services/:id/log-driver", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = (await request.json()) as Record<string, unknown>;
      service.Spec.TaskTemplate.LogDriver = {
        ...service.Spec.TaskTemplate.LogDriver,
        ...body,
      } as any;
      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse({ logDriver: service.Spec.TaskTemplate.LogDriver });
    }),

    http.patch("*/services/:id/healthcheck", async ({ params, request }) => {
      const service = dataset.servicesByID.get(params.id as string);

      if (!service) {
        return notFound();
      }

      const body = await request.json();

      if (service.Spec.TaskTemplate.ContainerSpec) {
        service.Spec.TaskTemplate.ContainerSpec.Healthcheck = body as any;
      }

      service.Version.Index++;
      service.UpdatedAt = new Date().toISOString();
      broadcastServiceUpdate(service);
      return jsonResponse({
        healthcheck: service.Spec.TaskTemplate.ContainerSpec?.Healthcheck,
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
