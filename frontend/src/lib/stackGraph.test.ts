import type { Config, Network, Secret, Service, StackDetail, Volume } from "../api/types";
import { stackToReactFlow } from "./stackGraph";
import type { Edge, Node } from "@xyflow/react";
import { describe, it, expect } from "vitest";

function makeService(name: string, task: Record<string, unknown> = {}): Service {
  return {
    ID: `svc-${name}`,
    Version: { Index: 1 },
    CreatedAt: "",
    UpdatedAt: "",
    Spec: {
      Name: name,
      TaskTemplate: { ContainerSpec: { Image: `${name}:1` }, ...task },
      Mode: { Replicated: { Replicas: 2 } },
    },
  } as unknown as Service;
}

function makeNetwork(name: string): Network {
  return { Id: `net-${name}`, Name: name, Driver: "overlay", Scope: "swarm" } as unknown as Network;
}

function makeConfig(name: string): Config {
  return { ID: `cfg-${name}`, Spec: { Name: name } } as unknown as Config;
}

function makeSecret(name: string): Secret {
  return { ID: `sec-${name}`, Spec: { Name: name } } as unknown as Secret;
}

function makeVolume(name: string): Volume {
  return { Name: name, Driver: "local", Scope: "local" } as unknown as Volume;
}

function makeStack(overrides: Partial<StackDetail> = {}): StackDetail {
  return {
    name: "shop",
    services: [],
    configs: [],
    secrets: [],
    networks: [],
    volumes: [],
    ...overrides,
  };
}

function nodeById(nodes: Node[], id: string): Node {
  const found = nodes.find((node) => node.id === id);

  if (!found) {
    throw new Error(`no node ${id} in ${nodes.map((node) => node.id).join(", ")}`);
  }

  return found;
}

function connections(edges: Edge[]): string[] {
  return edges.map((edge) => `${edge.source}->${edge.target}`);
}

describe("stackToReactFlow", () => {
  it("records where a mount lands and what a service answers to", () => {
    const { nodes } = stackToReactFlow(
      makeStack({
        services: [
          makeService("web", {
            ContainerSpec: {
              Image: "web:1",
              Secrets: [{ SecretID: "sec-token", SecretName: "token" }],
              Configs: [
                { ConfigID: "cfg-nginx", ConfigName: "nginx", File: { Name: "/etc/nginx.conf" } },
              ],
              Mounts: [{ Type: "volume", Source: "data", Target: "/var/lib/data", ReadOnly: true }],
            },
            Networks: [{ Target: "net-backend", Aliases: ["web", "web.internal"] }],
          }),
        ],
        networks: [makeNetwork("backend")],
        configs: [makeConfig("nginx")],
        secrets: [makeSecret("token")],
        volumes: [makeVolume("data")],
      }),
    );

    const data = (id: string) => nodes.find((node) => node.id === id)?.data as never;

    // A config names its own path; a secret takes Docker's default.
    expect(data("config:cfg-nginx")["mountedBy"]).toEqual([
      { service: "web", path: "/etc/nginx.conf" },
    ]);
    expect(data("secret:sec-token")["mountedBy"]).toEqual([
      { service: "web", path: "/run/secrets/token" },
    ]);
    expect(data("volume:data")["mountedBy"]).toEqual([
      { service: "web", path: "/var/lib/data (read-only)" },
    ]);
    expect(data("network:net-backend")["aliases"]).toEqual([
      { service: "web", names: ["web", "web.internal"] },
    ]);
  });

  it("creates a node for every resource the stack holds", () => {
    const { nodes } = stackToReactFlow(
      makeStack({
        services: [makeService("web")],
        networks: [makeNetwork("backend")],
        configs: [makeConfig("nginx")],
        secrets: [makeSecret("token")],
        volumes: [makeVolume("data")],
      }),
    );

    expect(nodes.map((node) => node.id).sort()).toEqual([
      "config:cfg-nginx",
      "network:net-backend",
      "secret:sec-token",
      "service:svc-web",
      "volume:data",
    ]);
  });

  it("connects a network to every service attached to it", () => {
    const { edges } = stackToReactFlow(
      makeStack({
        services: [
          makeService("web", { Networks: [{ Target: "net-backend" }] }),
          makeService("api", { Networks: [{ Target: "net-backend" }] }),
        ],
        networks: [makeNetwork("backend")],
      }),
    );

    expect(connections(edges)).toEqual([
      "network:net-backend->service:svc-web",
      "network:net-backend->service:svc-api",
    ]);
  });

  it("connects a service to the configs and secrets it mounts", () => {
    const { edges } = stackToReactFlow(
      makeStack({
        services: [
          makeService("web", {
            ContainerSpec: {
              Image: "web:1",
              Configs: [{ ConfigID: "cfg-nginx", ConfigName: "nginx" }],
              Secrets: [{ SecretID: "sec-token", SecretName: "token" }],
            },
          }),
        ],
        configs: [makeConfig("nginx")],
        secrets: [makeSecret("token")],
      }),
    );

    expect(connections(edges)).toEqual([
      "service:svc-web->config:cfg-nginx",
      "service:svc-web->secret:sec-token",
    ]);
  });

  it("matches a volume mount by its source name rather than an id", () => {
    const { edges } = stackToReactFlow(
      makeStack({
        services: [
          makeService("db", {
            ContainerSpec: {
              Image: "db:1",
              Mounts: [{ Type: "volume", Source: "data", Target: "/var/lib" }],
            },
          }),
        ],
        volumes: [makeVolume("data")],
      }),
    );

    expect(connections(edges)).toEqual(["service:svc-db->volume:data"]);
  });

  it("ignores a bind mount, which names a host path and not a volume", () => {
    const { nodes, edges } = stackToReactFlow(
      makeStack({
        services: [
          makeService("db", {
            ContainerSpec: {
              Image: "db:1",
              Mounts: [{ Type: "bind", Source: "/etc/localtime", Target: "/etc/localtime" }],
            },
          }),
        ],
      }),
    );

    expect(edges).toHaveLength(0);
    expect(nodes.map((node) => node.id)).toEqual(["service:svc-db"]);
  });

  it("marks a network the service uses but the stack does not hold as external", () => {
    const { nodes } = stackToReactFlow(
      makeStack({
        services: [makeService("web", { Networks: [{ Target: "net-traefik-public" }] })],
      }),
    );

    expect(nodeById(nodes, "network:net-traefik-public").data).toMatchObject({ external: true });
  });

  it("keeps a resource nothing references, marked unreferenced", () => {
    const { nodes } = stackToReactFlow(
      makeStack({
        services: [makeService("web")],
        configs: [makeConfig("orphan")],
      }),
    );

    expect(nodeById(nodes, "config:cfg-orphan").data).toMatchObject({ referenced: false });
  });

  it("links every node to the resource it stands for", () => {
    const { nodes } = stackToReactFlow(
      makeStack({
        services: [
          makeService("web", {
            ContainerSpec: {
              Image: "web:1",
              Configs: [{ ConfigID: "cfg-nginx", ConfigName: "nginx" }],
              Secrets: [{ SecretID: "sec-token", SecretName: "token" }],
              Mounts: [{ Type: "volume", Source: "shop_data", Target: "/data" }],
            },
            Networks: [{ Target: "net-backend" }],
          }),
        ],
        networks: [makeNetwork("backend")],
      }),
    );

    expect(nodeById(nodes, "service:svc-web").data).toMatchObject({ href: "/services/svc-web" });
    expect(nodeById(nodes, "network:net-backend").data).toMatchObject({
      href: "/networks/net-backend",
    });
    expect(nodeById(nodes, "config:cfg-nginx").data).toMatchObject({ href: "/configs/cfg-nginx" });
    expect(nodeById(nodes, "secret:sec-token").data).toMatchObject({ href: "/secrets/sec-token" });
  });

  it("links a volume by its full name, which the label strips for display", () => {
    const { nodes } = stackToReactFlow(
      makeStack({
        services: [
          makeService("db", {
            ContainerSpec: {
              Image: "db:1",
              Mounts: [{ Type: "volume", Source: "shop_data", Target: "/data" }],
            },
          }),
        ],
        volumes: [makeVolume("shop_data")],
      }),
    );

    expect(nodeById(nodes, "volume:shop_data").data).toMatchObject({
      name: "data",
      href: "/volumes/shop_data",
    });
  });

  it("strips the stack's own prefix from the names it shows", () => {
    const { nodes } = stackToReactFlow(
      makeStack({
        services: [makeService("shop_web")],
        networks: [makeNetwork("shop_backend")],
        volumes: [makeVolume("shop_data")],
      }),
    );

    expect(nodeById(nodes, "service:svc-shop_web").data).toMatchObject({ name: "web" });
    expect(nodeById(nodes, "network:net-shop_backend").data).toMatchObject({ name: "backend" });
    expect(nodeById(nodes, "volume:shop_data").data).toMatchObject({ name: "data" });
  });

  it("carries the service's image and replicas onto its node", () => {
    const { nodes } = stackToReactFlow(makeStack({ services: [makeService("web")] }));

    expect(nodeById(nodes, "service:svc-web").data).toMatchObject({
      name: "web",
      image: "web:1",
      replicas: 2,
      mode: "replicated",
    });
  });
});
