import type { JGFGraph, JGFMetadata } from "../api/types";
import { networkGraphToReactFlow, placementGraphToReactFlow } from "./topologyTransform";
import { describe, it, expect } from "vitest";

const baseMetadata: JGFMetadata = { "@context": "https://example.com" };

function makeNetworkGraph(overrides: Partial<JGFGraph> = {}): JGFGraph {
  return {
    id: "network",
    type: "network-topology",
    label: "Network Topology",
    directed: false,
    metadata: baseMetadata,
    nodes: {},
    edges: [],
    hyperedges: [],
    ...overrides,
  };
}

describe("networkGraphToReactFlow", () => {
  it("creates group nodes for stacks and service nodes as children", () => {
    const graph = makeNetworkGraph({
      nodes: {
        "urn:cetacean:service:s1": {
          label: "web",
          metadata: {
            ...baseMetadata,
            kind: "service",
            replicas: 3,
            image: "nginx:1.25",
            mode: "replicated",
            ports: ["80:8080/tcp"],
          },
        },
        "urn:cetacean:service:s2": {
          label: "api",
          metadata: {
            ...baseMetadata,
            kind: "service",
            replicas: 2,
            image: "node:20",
            mode: "replicated",
          },
        },
        "urn:cetacean:service:s3": {
          label: "monitor",
          metadata: {
            ...baseMetadata,
            kind: "service",
            replicas: 1,
            image: "prom:latest",
            mode: "replicated",
          },
        },
      },
      edges: [
        {
          source: "urn:cetacean:service:s1",
          target: "urn:cetacean:service:s2",
          metadata: {
            ...baseMetadata,
            networks: [{ id: "net1", name: "app_net", driver: "overlay", scope: "swarm" }],
          },
        },
      ],
      hyperedges: [
        {
          nodes: ["urn:cetacean:service:s1", "urn:cetacean:service:s2"],
          metadata: { ...baseMetadata, kind: "stack", name: "app" },
        },
      ],
    });

    const { nodes, edges } = networkGraphToReactFlow(graph);

    const groups = nodes.filter(({ type }) => type === "stackGroup");
    const services = nodes.filter(({ type }) => type === "serviceCard");
    expect(groups.length).toBe(1);
    expect(groups[0].data.label).toBe("app");
    expect(services.length).toBe(3);
    expect(services.filter(({ parentId }) => parentId === "stack:app").length).toBe(2);
    expect(
      services.find(({ id }) => id === "urn:cetacean:service:s3")?.parentId,
    ).toBeUndefined();

    // One edge with all networks collapsed
    expect(edges.length).toBe(1);
    expect((edges[0].data as { networks: Array<{ name: string }> }).networks[0].name).toBe(
      "app_net",
    );
  });

  it("collapses multiple networks into a single edge per pair", () => {
    const graph = makeNetworkGraph({
      nodes: {
        "urn:cetacean:service:s1": {
          label: "web",
          metadata: { ...baseMetadata, kind: "service", replicas: 1, image: "nginx", mode: "replicated" },
        },
        "urn:cetacean:service:s2": {
          label: "api",
          metadata: { ...baseMetadata, kind: "service", replicas: 1, image: "node", mode: "replicated" },
        },
      },
      edges: [
        {
          source: "urn:cetacean:service:s1",
          target: "urn:cetacean:service:s2",
          metadata: {
            ...baseMetadata,
            networks: [
              { id: "net1", name: "backend", driver: "overlay", scope: "swarm" },
              { id: "net2", name: "frontend", driver: "overlay", scope: "swarm" },
            ],
          },
        },
      ],
    });

    const { edges } = networkGraphToReactFlow(graph);
    expect(edges.length).toBe(1);
    const names = (edges[0].data as { networks: Array<{ name: string }> }).networks
      .map(({ name }) => name)
      .sort();
    expect(names).toEqual(["backend", "frontend"]);
  });

  it("deduplicates bidirectional edges into a single edge", () => {
    const graph = makeNetworkGraph({
      nodes: {
        "urn:cetacean:service:s1": {
          label: "web",
          metadata: { ...baseMetadata, kind: "service", replicas: 1, image: "nginx", mode: "replicated" },
        },
        "urn:cetacean:service:s2": {
          label: "api",
          metadata: { ...baseMetadata, kind: "service", replicas: 1, image: "node", mode: "replicated" },
        },
      },
      edges: [
        {
          source: "urn:cetacean:service:s1",
          target: "urn:cetacean:service:s2",
          metadata: {
            ...baseMetadata,
            networks: [{ id: "net1", name: "backend", driver: "overlay", scope: "swarm" }],
          },
        },
        {
          source: "urn:cetacean:service:s2",
          target: "urn:cetacean:service:s1",
          metadata: {
            ...baseMetadata,
            networks: [{ id: "net1", name: "backend", driver: "overlay", scope: "swarm" }],
          },
        },
      ],
    });

    const { edges } = networkGraphToReactFlow(graph);
    expect(edges.length).toBe(1);
  });

  it("merges networks from bidirectional edges", () => {
    const graph = makeNetworkGraph({
      nodes: {
        "urn:cetacean:service:s1": {
          label: "web",
          metadata: { ...baseMetadata, kind: "service", replicas: 1, image: "nginx", mode: "replicated" },
        },
        "urn:cetacean:service:s2": {
          label: "api",
          metadata: { ...baseMetadata, kind: "service", replicas: 1, image: "node", mode: "replicated" },
        },
      },
      edges: [
        {
          source: "urn:cetacean:service:s1",
          target: "urn:cetacean:service:s2",
          metadata: {
            ...baseMetadata,
            networks: [{ id: "net1", name: "backend", driver: "overlay", scope: "swarm" }],
          },
        },
        {
          source: "urn:cetacean:service:s2",
          target: "urn:cetacean:service:s1",
          metadata: {
            ...baseMetadata,
            networks: [{ id: "net2", name: "frontend", driver: "overlay", scope: "swarm" }],
          },
        },
      ],
    });

    const { edges } = networkGraphToReactFlow(graph);
    expect(edges.length).toBe(1);
    const names = (edges[0].data as { networks: Array<{ name: string }> }).networks
      .map(({ name }) => name)
      .sort();
    expect(names).toEqual(["backend", "frontend"]);
  });
});

describe("placementGraphToReactFlow", () => {
  it("aggregates tasks by service within each node", () => {
    const graph: JGFGraph = {
      id: "placement",
      type: "placement-topology",
      label: "Placement Topology",
      directed: false,
      metadata: baseMetadata,
      nodes: {
        "urn:cetacean:node:n1": {
          label: "worker-01",
          metadata: { ...baseMetadata, kind: "node", role: "worker", state: "ready", availability: "active" },
        },
        "urn:cetacean:node:n2": {
          label: "worker-02",
          metadata: { ...baseMetadata, kind: "node", role: "worker", state: "ready", availability: "active" },
        },
        "urn:cetacean:service:svc1": {
          label: "web",
          metadata: { ...baseMetadata, kind: "service" },
        },
        "urn:cetacean:service:svc2": {
          label: "api",
          metadata: { ...baseMetadata, kind: "service" },
        },
      },
      hyperedges: [
        {
          nodes: ["urn:cetacean:service:svc1", "urn:cetacean:node:n1", "urn:cetacean:node:n2"],
          metadata: {
            ...baseMetadata,
            kind: "placement",
            tasks: [
              { id: "t1", node: "urn:cetacean:node:n1", state: "running", slot: 1, image: "nginx:1.25" },
              { id: "t2", node: "urn:cetacean:node:n1", state: "running", slot: 2, image: "nginx:1.25" },
              { id: "t4", node: "urn:cetacean:node:n2", state: "running", slot: 3, image: "nginx:1.25" },
            ],
          },
        },
        {
          nodes: ["urn:cetacean:service:svc2", "urn:cetacean:node:n1"],
          metadata: {
            ...baseMetadata,
            kind: "placement",
            tasks: [
              { id: "t3", node: "urn:cetacean:node:n1", state: "running", slot: 1, image: "node:20" },
            ],
          },
        },
      ],
    };

    const { nodes } = placementGraphToReactFlow(graph);

    expect(nodes.length).toBe(2);
    expect(nodes[0].type).toBe("physicalNode");

    const n1Data = nodes[0].data as {
      services: { serviceId: string; running: number; total: number }[];
    };
    expect(n1Data.services.length).toBe(2);
    const webSvc = n1Data.services.find(({ serviceId }) => serviceId === "urn:cetacean:service:svc1");
    expect(webSvc!.running).toBe(2);
    expect(webSvc!.total).toBe(2);

    const n2Data = nodes[1].data as { services: { total: number }[] };
    expect(n2Data.services.length).toBe(1);
    expect(n2Data.services[0].total).toBe(1);
  });
});
