import type { Node, Edge } from "@xyflow/react";
import type { NetworkTopology, PlacementTopology } from "@/api/types";

const COLORS = [
  "#3b82f6",
  "#ef4444",
  "#10b981",
  "#f59e0b",
  "#8b5cf6",
  "#ec4899",
  "#06b6d4",
  "#f97316",
  "#6366f1",
  "#14b8a6",
  "#e11d48",
  "#84cc16",
];

function hashColor(id: string): string {
  let h = 0;
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) | 0;
  }
  return COLORS[Math.abs(h) % COLORS.length];
}

function stripStackPrefix(name: string, stack?: string): string {
  if (stack && name.startsWith(stack + "_")) {
    return name.slice(stack.length + 1);
  }
  return name;
}

export function buildLogicalFlow(data: NetworkTopology): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  const networkMap = new Map(data.networks.map((n) => [n.id, n]));

  // Collect unique stacks and assign colors
  const stacks = new Set<string>();
  for (const svc of data.nodes) {
    if (svc.stack) stacks.add(svc.stack);
  }

  const stackColorMap = new Map<string, string>();
  for (const stack of stacks) {
    stackColorMap.set(stack, hashColor(stack));
  }

  // Build connected service set (services that have at least one edge)
  const connectedSources = new Set<string>();
  const connectedTargets = new Set<string>();
  for (const edge of data.edges) {
    connectedSources.add(edge.source);
    connectedTargets.add(edge.target);
  }

  // Create stack group nodes
  for (const stack of stacks) {
    nodes.push({
      id: `stack:${stack}`,
      type: "stackGroup",
      position: { x: 0, y: 0 },
      data: { label: stack, variant: "stack", color: stackColorMap.get(stack) },
    });
  }

  // Create service nodes
  for (const svc of data.nodes) {
    const node: Node = {
      id: svc.id,
      type: "serviceCard",
      position: { x: 0, y: 0 },
      data: {
        id: svc.id,
        name: stripStackPrefix(svc.name, svc.stack),
        mode: svc.mode,
        image: svc.image,
        replicas: svc.replicas,
        ports: svc.ports,
        updateStatus: svc.updateStatus,
        stackColor: svc.stack ? stackColorMap.get(svc.stack) : undefined,
        hasSourceEdge: connectedSources.has(svc.id),
        hasTargetEdge: connectedTargets.has(svc.id),
      },
    };
    if (svc.stack) {
      node.parentId = `stack:${svc.stack}`;
    }
    nodes.push(node);
  }

  // Create edges: one per network per API edge, with parallel offsets
  for (const edge of data.edges) {
    const count = edge.networks.length;
    edge.networks.forEach((netId, index) => {
      const net = networkMap.get(netId);
      edges.push({
        id: `net:${netId}:${edge.source}:${edge.target}`,
        source: edge.source,
        target: edge.target,
        type: "networkEdge",
        data: {
          color: hashColor(netId),
          networkName: net?.name ?? netId,
          networkDriver: net?.driver ?? "unknown",
          parallelIndex: index,
          parallelCount: count,
        },
      });
    });
  }

  return { nodes, edges };
}

export function buildPhysicalFlow(data: PlacementTopology): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [];

  for (const clusterNode of data.nodes) {
    nodes.push({
      id: clusterNode.id,
      type: "nodeGroup",
      position: { x: 0, y: 0 },
      data: {
        label: clusterNode.hostname,
        role: clusterNode.role,
        state: clusterNode.state,
        availability: clusterNode.availability,
        variant: "node",
      },
    });

    for (const task of clusterNode.tasks) {
      nodes.push({
        id: task.id,
        type: "taskCard",
        position: { x: 0, y: 0 },
        parentId: clusterNode.id,
        data: {
          id: task.id,
          serviceId: task.serviceId,
          serviceName: task.serviceName,
          slot: task.slot,
          state: task.state,
          image: task.image,
          highlighted: false,
          onHoverService: () => {},
        },
      });
    }
  }

  return { nodes, edges: [] };
}
