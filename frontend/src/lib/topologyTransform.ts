import type { NetworkTopology, PlacementTopology } from "../api/types";
import { getChartColor } from "./chartColors";
import type { Edge, Node } from "@xyflow/react";

export function hashColor(id: string): string {
  let hash = 0;

  for (let index = 0; index < id.length; index++) {
    hash = (hash * 31 + id.charCodeAt(index)) | 0;
  }

  return getChartColor(Math.abs(hash));
}

function stripStackPrefix(name: string, stack?: string): string {
  if (stack && name.startsWith(stack + "_")) {
    return name.slice(stack.length + 1);
  }

  return name;
}

/** Estimate rendered card height for ELK layout (matches ServiceCardNode CSS). */
function estimateCardHeight(ports?: string[], updateStatus?: string): number {
  // base: border(4) + p-3(24) + name(20) + mb(4) + image(16) + mb(4) + replicas(16) + mb(4)
  let height = 92;

  if (ports && ports.length > 0) {
    height += ports.length * 16;
  }

  if (updateStatus) {
    height += 20;
  }

  return height;
}

function byId(a: { id: string }, b: { id: string }): number {
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

export function buildLogicalFlow(data: NetworkTopology): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  const networkMap = new Map(data.networks.map((node) => [node.id, node]));
  const serviceMap = new Map(data.nodes.map((service) => [service.id, service]));

  // Collect unique stacks and assign colors
  const stacks = new Set<string>();

  for (const { stack } of data.nodes) {
    if (stack) {
      stacks.add(stack);
    }
  }

  const stackColorMap = new Map<string, string>();

  for (const stack of stacks) {
    stackColorMap.set(stack, hashColor(stack));
  }

  // Normalize edge directions: smaller ID is always source (edges are symmetric
  // network connections, but stable direction is needed for deterministic layout).
  const normalizedEdges = data.edges.map((edge) =>
    edge.source <= edge.target
      ? edge
      : {
          ...edge,
          source: edge.target,
          target: edge.source,
        },
  );

  // Build connected service set (services that have at least one edge)
  const connectedSources = new Set<string>();
  const connectedTargets = new Set<string>();

  for (const { source, target } of normalizedEdges) {
    connectedSources.add(source);
    connectedTargets.add(target);
  }

  // Create stack group nodes (sorted for deterministic layout)
  const sortedStacks = [...stacks].sort();

  for (const stack of sortedStacks) {
    nodes.push({
      id: `stack:${stack}`,
      type: "stackGroup",
      position: { x: 0, y: 0 },
      data: { label: stack, variant: "stack", color: stackColorMap.get(stack) },
    });
  }

  // Create service nodes (sorted by ID for deterministic layout)
  const sortedServices = [...data.nodes].sort(byId);

  for (const { id, image, mode, name, ports, replicas, stack, updateStatus } of sortedServices) {
    const node: Node = {
      id,
      type: "serviceCard",
      position: { x: 0, y: 0 },
      data: {
        id,
        name: stripStackPrefix(name, stack),
        mode,
        image,
        replicas,
        ports,
        updateStatus,
        stackColor: stack ? stackColorMap.get(stack) : undefined,
        hasSourceEdge: connectedSources.has(id),
        hasTargetEdge: connectedTargets.has(id),
        _elkHeight: estimateCardHeight(ports, updateStatus),
      },
    };

    if (stack) {
      node.parentId = `stack:${stack}`;
    }

    nodes.push(node);
  }

  // Deduplicate normalized edges: merge edges sharing the same source-target pair,
  // combining their network sets (the API may return both A→B and B→A).
  const edgeMap = new Map<string, { source: string; target: string; networks: Set<string> }>();

  for (const edge of normalizedEdges) {
    const key = `${edge.source}:${edge.target}`;
    const existing = edgeMap.get(key);

    if (existing) {
      for (const netId of edge.networks) {
        existing.networks.add(netId);
      }
    } else {
      edgeMap.set(key, {
        source: edge.source,
        target: edge.target,
        networks: new Set(edge.networks),
      });
    }
  }

  // Create one edge per unique source-target pair
  const sortedEdgeKeys = [...edgeMap.keys()].sort();
  for (const key of sortedEdgeKeys) {
    const edge = edgeMap.get(key)!;
    const srcSvc = serviceMap.get(edge.source);
    const tgtSvc = serviceMap.get(edge.target);
    const networks = [...edge.networks].sort().map((netId) => {
      const net = networkMap.get(netId);

      return {
        id: netId,
        name: net?.name ?? netId,
        driver: net?.driver ?? "unknown",
        scope: net?.scope ?? "swarm",
        stack: net?.stack,
        color: net?.stack ? (stackColorMap.get(net.stack) ?? hashColor(netId)) : undefined,
      };
    });

    // Collect non-default aliases per endpoint (exclude aliases matching the service name)
    const sourceAliases: string[] = [];
    const targetAliases: string[] = [];

    for (const netId of edge.networks) {
      for (const alias of srcSvc?.networkAliases?.[netId] ?? []) {
        if (alias !== srcSvc?.name && !sourceAliases.includes(alias)) {
          sourceAliases.push(alias);
        }
      }

      for (const alias of tgtSvc?.networkAliases?.[netId] ?? []) {
        if (alias !== tgtSvc?.name && !targetAliases.includes(alias)) {
          targetAliases.push(alias);
        }
      }
    }

    edges.push({
      id: `edge:${edge.source}:${edge.target}`,
      source: edge.source,
      target: edge.target,
      type: "networkEdge",
      data: {
        networks,
        sourceAliases: sourceAliases.length > 0 ? sourceAliases : undefined,
        targetAliases: targetAliases.length > 0 ? targetAliases : undefined,
      },
    });
  }

  return { nodes, edges };
}

export function buildPhysicalFlow(data: PlacementTopology): { nodes: Node[] } {
  const nodes: Node[] = [];
  const columns = 3;
  const cardHeight = 80; // card: p-2.5(20) + name(18) + mb-0.5(2) + image(16) + mb-1(4) + tasks(18) + border(2)
  const cardGap = 8; // gap-2 between grid items
  const headerHeight = 44; // header line(20) + mb-3(12) + container p-4 top(16) - overlap adjustment
  const padding = 24; // container p-4 bottom(16) + extra breathing room
  const gap = 24;

  const sortedClusterNodes = [...data.nodes].sort(byId);
  let y = 0;

  for (const { availability, hostname, id, role, state, tasks } of sortedClusterNodes) {
    // Aggregate tasks by service
    const serviceMap = new Map<
      string,
      { serviceName: string; image: string; running: number; total: number; states: string[] }
    >();

    for (const task of tasks) {
      let entry = serviceMap.get(task.serviceId);

      if (!entry) {
        entry = {
          serviceName: task.serviceName,
          image: task.image,
          running: 0,
          total: 0,
          states: [],
        };

        serviceMap.set(task.serviceId, entry);
      }

      entry.total++;

      if (task.state === "running" || task.state === "complete") {
        entry.running++;
      }

      entry.states.push(task.state);
    }

    const services = [...serviceMap.entries()]
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      .map(([serviceId, service]) => ({ serviceId, ...service }));

    const rows = Math.max(1, Math.ceil(services.length / columns));
    const nodeHeight = headerHeight + rows * cardHeight + Math.max(0, rows - 1) * cardGap + padding;

    nodes.push({
      id,
      type: "physicalNode",
      position: { x: 0, y },
      data: {
        label: hostname,
        role,
        state,
        availability,
        services,
      },
    });

    y += nodeHeight + gap;
  }

  return { nodes };
}
