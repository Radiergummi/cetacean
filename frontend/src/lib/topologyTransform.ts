import type { JGFGraph } from "../api/types";
import { getChartColor } from "./chartColors";
import type { Edge, Node } from "@xyflow/react";

export function hashColor(id: string): string {
  let hash = 0;

  for (let index = 0; index < id.length; index++) {
    hash = (hash * 31 + id.charCodeAt(index)) | 0;
  }

  return getChartColor(Math.abs(hash));
}

export function stripStackPrefix(name: string, stack?: string): string {
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

export function networkGraphToReactFlow(graph: JGFGraph): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  // Build stack membership from hyperedges
  const stackMembers = new Map<string, Set<string>>();

  for (const hyperedge of graph.hyperedges ?? []) {
    if (hyperedge.metadata.kind === "stack") {
      const stackName = hyperedge.metadata.name as string;
      stackMembers.set(stackName, new Set(hyperedge.nodes));
    }
  }

  // Map service URN → stack name
  const serviceStack = new Map<string, string>();

  for (const [stackName, members] of stackMembers) {
    for (const urn of members) {
      serviceStack.set(urn, stackName);
    }
  }

  // Assign stack colors
  const stackColorMap = new Map<string, string>();

  for (const stack of [...stackMembers.keys()].sort()) {
    stackColorMap.set(stack, hashColor(stack));
  }

  // Normalize edge directions: smaller ID is always source
  const normalizedEdges = (graph.edges ?? []).map((edge) =>
    edge.source <= edge.target
      ? edge
      : {
          ...edge,
          source: edge.target,
          target: edge.source,
        },
  );

  // Build connected service set
  const connectedSources = new Set<string>();
  const connectedTargets = new Set<string>();

  for (const { source, target } of normalizedEdges) {
    connectedSources.add(source);
    connectedTargets.add(target);
  }

  // Create stack group nodes
  for (const stack of [...stackMembers.keys()].sort()) {
    nodes.push({
      id: `stack:${stack}`,
      type: "stackGroup",
      position: { x: 0, y: 0 },
      data: { label: stack, variant: "stack", color: stackColorMap.get(stack) },
    });
  }

  // Create service nodes sorted by URN
  const sortedEntries = Object.entries(graph.nodes)
    .map(([urn, jgfNode]) => ({ urn, jgfNode }))
    .sort((a, b) => (a.urn < b.urn ? -1 : a.urn > b.urn ? 1 : 0));

  for (const { urn, jgfNode } of sortedEntries) {
    const metadata = jgfNode.metadata;
    const stack = serviceStack.get(urn);
    const ports = metadata.ports as string[] | undefined;
    const updateStatus = metadata.updateStatus as string | undefined;

    const node: Node = {
      id: urn,
      type: "serviceCard",
      position: { x: 0, y: 0 },
      data: {
        id: urn,
        name: stripStackPrefix(jgfNode.label, stack),
        mode: metadata.mode as string,
        image: metadata.image as string,
        replicas: metadata.replicas as number,
        ports,
        updateStatus,
        stackColor: stack ? stackColorMap.get(stack) : undefined,
        hasSourceEdge: connectedSources.has(urn),
        hasTargetEdge: connectedTargets.has(urn),
        _elkHeight: estimateCardHeight(ports, updateStatus),
      },
    };

    if (stack) {
      node.parentId = `stack:${stack}`;
    }

    nodes.push(node);
  }

  // Deduplicate normalized edges: merge edges sharing the same source-target pair
  const edgeMap = new Map<
    string,
    {
      source: string;
      target: string;
      networks: {
        id: string;
        name: string;
        driver: string;
        scope: string;
        aliases?: Record<string, string[]>;
      }[];
    }
  >();

  for (const edge of normalizedEdges) {
    const key = `${edge.source}:${edge.target}`;
    const edgeNetworks = (edge.metadata.networks ?? []) as {
      id: string;
      name: string;
      driver: string;
      scope: string;
      aliases?: Record<string, string[]>;
    }[];
    const existing = edgeMap.get(key);

    if (existing) {
      for (const net of edgeNetworks) {
        if (!existing.networks.some(({ id }) => id === net.id)) {
          existing.networks.push(net);
        }
      }
    } else {
      edgeMap.set(key, {
        source: edge.source,
        target: edge.target,
        networks: [...edgeNetworks],
      });
    }
  }

  // Create one edge per unique source-target pair
  const sortedEdgeKeys = [...edgeMap.keys()].sort();

  for (const key of sortedEdgeKeys) {
    const edge = edgeMap.get(key)!;
    const networks = edge.networks
      .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
      .map((net) => ({
        id: net.id,
        name: net.name,
        driver: net.driver,
        scope: net.scope,
      }));

    // Collect non-default aliases per endpoint
    const sourceAliases: string[] = [];
    const targetAliases: string[] = [];
    const sourceName = graph.nodes[edge.source]?.label;
    const targetName = graph.nodes[edge.target]?.label;

    for (const net of edge.networks) {
      if (net.aliases) {
        for (const alias of net.aliases[edge.source] ?? []) {
          if (alias !== sourceName && !sourceAliases.includes(alias)) {
            sourceAliases.push(alias);
          }
        }

        for (const alias of net.aliases[edge.target] ?? []) {
          if (alias !== targetName && !targetAliases.includes(alias)) {
            targetAliases.push(alias);
          }
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

export function placementGraphToReactFlow(graph: JGFGraph): { nodes: Node[] } {
  const nodes: Node[] = [];
  const columns = 3;
  const cardHeight = 80;
  const cardGap = 8;
  const headerHeight = 44;
  const padding = 24;
  const gap = 24;

  // Separate cluster nodes from service nodes
  const clusterNodes: { urn: string; label: string; metadata: Record<string, unknown> }[] = [];

  for (const [urn, jgfNode] of Object.entries(graph.nodes)) {
    if (jgfNode.metadata.kind === "node") {
      clusterNodes.push({ urn, label: jgfNode.label, metadata: jgfNode.metadata });
    }
  }

  // Build tasks grouped by node URN from hyperedges
  const tasksByNode = new Map<
    string,
    {
      serviceUrn: string;
      serviceName: string;
      tasks: { id: string; node: string; state: string; slot: number; image: string }[];
    }[]
  >();

  for (const hyperedge of graph.hyperedges ?? []) {
    if (hyperedge.metadata.kind !== "placement") {
      continue;
    }

    const serviceUrn = hyperedge.nodes[0];
    const serviceName = graph.nodes[serviceUrn]?.label ?? serviceUrn;
    const tasks = (hyperedge.metadata.tasks ?? []) as {
      id: string;
      node: string;
      state: string;
      slot: number;
      image: string;
    }[];

    // Group tasks by node URN
    const byNode = new Map<string, typeof tasks>();

    for (const task of tasks) {
      const nodeUrn = task.node;
      let list = byNode.get(nodeUrn);

      if (!list) {
        list = [];
        byNode.set(nodeUrn, list);
      }

      list.push(task);
    }

    for (const [nodeUrn, nodeTasks] of byNode) {
      let entries = tasksByNode.get(nodeUrn);

      if (!entries) {
        entries = [];
        tasksByNode.set(nodeUrn, entries);
      }

      entries.push({ serviceUrn, serviceName, tasks: nodeTasks });
    }
  }

  const sortedClusterNodes = [...clusterNodes].sort((a, b) =>
    a.urn < b.urn ? -1 : a.urn > b.urn ? 1 : 0,
  );

  let y = 0;

  for (const { urn, label, metadata } of sortedClusterNodes) {
    // Aggregate tasks by service for this node
    const serviceEntries = tasksByNode.get(urn) ?? [];
    const serviceMap = new Map<
      string,
      { serviceName: string; image: string; running: number; total: number; states: string[] }
    >();

    for (const { serviceUrn, serviceName, tasks } of serviceEntries) {
      let entry = serviceMap.get(serviceUrn);

      if (!entry) {
        entry = {
          serviceName,
          image: tasks[0]?.image ?? "",
          running: 0,
          total: 0,
          states: [],
        };

        serviceMap.set(serviceUrn, entry);
      }

      for (const task of tasks) {
        entry.total++;

        if (task.state === "running" || task.state === "complete") {
          entry.running++;
        }

        entry.states.push(task.state);
      }
    }

    const services = [...serviceMap.entries()]
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
      .map(([serviceId, service]) => ({ serviceId, ...service }));

    const rows = Math.max(1, Math.ceil(services.length / columns));
    const nodeHeight = headerHeight + rows * cardHeight + Math.max(0, rows - 1) * cardGap + padding;

    nodes.push({
      id: urn,
      type: "physicalNode",
      position: { x: 0, y },
      data: {
        label,
        role: metadata.role as string,
        state: metadata.state as string,
        availability: metadata.availability as string,
        services,
      },
    });

    y += nodeHeight + gap;
  }

  return { nodes };
}
