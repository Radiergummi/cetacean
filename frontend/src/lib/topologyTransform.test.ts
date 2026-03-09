import { describe, it, expect } from "vitest";
import { buildLogicalFlow, buildPhysicalFlow } from "./topologyTransform";
import type { NetworkTopology, PlacementTopology } from "@/api/types";

describe("buildLogicalFlow", () => {
  it("creates group nodes for stacks and service nodes as children", () => {
    const data: NetworkTopology = {
      nodes: [
        {
          id: "s1",
          name: "web",
          stack: "app",
          replicas: 3,
          image: "nginx:1.25",
          mode: "replicated",
          ports: ["80:8080/tcp"],
        },
        { id: "s2", name: "api", stack: "app", replicas: 2, image: "node:20", mode: "replicated" },
        { id: "s3", name: "monitor", replicas: 1, image: "prom:latest", mode: "replicated" },
      ],
      edges: [{ source: "s1", target: "s2", networks: ["net1"] }],
      networks: [{ id: "net1", name: "app_net", driver: "overlay", scope: "swarm" }],
    };
    const { nodes, edges } = buildLogicalFlow(data);

    const groups = nodes.filter((n) => n.type === "stackGroup");
    const services = nodes.filter((n) => n.type === "serviceCard");
    expect(groups.length).toBe(1);
    expect(groups[0].data.label).toBe("app");
    expect(services.length).toBe(3);
    expect(services.filter((n) => n.parentId === "stack:app").length).toBe(2);
    expect(services.find((n) => n.id === "s3")?.parentId).toBeUndefined();

    // One edge with all networks collapsed
    expect(edges.length).toBe(1);
    expect((edges[0].data as { networks: Array<{ name: string }> }).networks[0].name).toBe("app_net");
  });

  it("collapses multiple networks into a single edge per pair", () => {
    const data: NetworkTopology = {
      nodes: [
        { id: "s1", name: "web", replicas: 1, image: "nginx", mode: "replicated" },
        { id: "s2", name: "api", replicas: 1, image: "node", mode: "replicated" },
      ],
      edges: [{ source: "s1", target: "s2", networks: ["net1", "net2"] }],
      networks: [
        { id: "net1", name: "backend", driver: "overlay", scope: "swarm" },
        { id: "net2", name: "frontend", driver: "overlay", scope: "swarm" },
      ],
    };
    const { edges } = buildLogicalFlow(data);
    expect(edges.length).toBe(1);
    const names = (edges[0].data as { networks: Array<{ name: string }> }).networks.map((n) => n.name).sort();
    expect(names).toEqual(["backend", "frontend"]);
  });
});

describe("buildPhysicalFlow", () => {
  it("creates node groups with task children", () => {
    const data: PlacementTopology = {
      nodes: [
        {
          id: "n1",
          hostname: "worker-01",
          role: "worker",
          state: "ready",
          availability: "active",
          tasks: [
            {
              id: "t1",
              serviceId: "svc1",
              serviceName: "web",
              state: "running",
              slot: 1,
              image: "nginx:1.25",
            },
            {
              id: "t2",
              serviceId: "svc1",
              serviceName: "web",
              state: "running",
              slot: 2,
              image: "nginx:1.25",
            },
          ],
        },
        {
          id: "n2",
          hostname: "worker-02",
          role: "worker",
          state: "ready",
          availability: "active",
          tasks: [
            {
              id: "t3",
              serviceId: "svc1",
              serviceName: "web",
              state: "running",
              slot: 3,
              image: "nginx:1.25",
            },
          ],
        },
      ],
    };
    const { nodes, edges } = buildPhysicalFlow(data);

    const groups = nodes.filter((n) => n.type === "nodeGroup");
    const tasks = nodes.filter((n) => n.type === "taskCard");
    expect(groups.length).toBe(2);
    expect(tasks.length).toBe(3);
    expect(tasks.filter((n) => n.parentId === "n1").length).toBe(2);
    expect(edges.length).toBe(0);
  });
});
