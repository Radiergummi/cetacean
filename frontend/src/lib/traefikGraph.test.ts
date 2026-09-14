import type { TraefikIntegration } from "../api/types";
import { traefikIntegrationToReactFlow } from "./traefikGraph";
import type { Edge, Node } from "@xyflow/react";
import { describe, it, expect } from "vitest";

function makeIntegration(overrides: Partial<TraefikIntegration> = {}): TraefikIntegration {
  return { name: "traefik", enabled: true, ...overrides };
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

describe("traefikIntegrationToReactFlow", () => {
  it("creates one entrypoint node per distinct name across routers", () => {
    const { nodes, edges } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [
          { name: "web", entrypoints: ["web", "websecure"], service: "api" },
          { name: "admin", entrypoints: ["websecure"], service: "api" },
        ],
        services: [{ name: "api" }],
      }),
    );

    expect(
      nodes.filter((node) => node.type === "traefikEntrypoint").map((node) => node.id),
    ).toEqual(["entrypoint:web", "entrypoint:websecure"]);
    expect(connections(edges)).toContain("entrypoint:websecure->router:web");
    expect(connections(edges)).toContain("entrypoint:websecure->router:admin");
  });

  it("chains middlewares in declared order between the router and its service", () => {
    const { edges } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web", middlewares: ["redirect", "auth"], service: "api" }],
        services: [{ name: "api" }],
        middlewares: [
          { name: "auth", type: "basicauth" },
          { name: "redirect", type: "redirectscheme" },
        ],
      }),
    );

    expect(connections(edges)).toEqual([
      "router:web->middleware:redirect",
      "middleware:redirect->middleware:auth",
      "middleware:auth->service:api",
    ]);
  });

  it("connects the router straight to its service when it declares no middlewares", () => {
    const { edges } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web", service: "api" }],
        services: [{ name: "api" }],
      }),
    );

    expect(connections(edges)).toEqual(["router:web->service:api"]);
  });

  it("marks a middleware that is referenced but not declared here as external", () => {
    const { nodes } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web", middlewares: ["auth@file"], service: "api" }],
        services: [{ name: "api" }],
      }),
    );

    expect(nodeById(nodes, "middleware:auth@file").data).toMatchObject({
      name: "auth@file",
      external: true,
    });
  });

  it("binds a router that names no service to the only service declared here", () => {
    const { edges, nodes } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web" }],
        services: [{ name: "api", port: 8080 }],
      }),
    );

    expect(connections(edges)).toEqual(["router:web->service:api"]);
    expect(nodeById(nodes, "service:api").data).toMatchObject({ implicit: true });
  });

  it("gives a router that names no service an unresolved node when several are declared", () => {
    const { edges, nodes } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web" }],
        services: [{ name: "api" }, { name: "metrics" }],
      }),
    );

    expect(connections(edges)).toContain("router:web->service:unresolved:web");
    expect(nodeById(nodes, "service:unresolved:web").data).toMatchObject({ origin: "unresolved" });
  });

  it("marks a service the router names but does not declare here as external", () => {
    const { nodes } = traefikIntegrationToReactFlow(
      makeIntegration({ routers: [{ name: "web", service: "api@file" }] }),
    );

    expect(nodeById(nodes, "service:api@file").data).toMatchObject({ origin: "external" });
  });

  it("keeps a declared service that no router references, marked unreferenced", () => {
    const { nodes, edges } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web", service: "api" }],
        services: [{ name: "api" }, { name: "metrics" }],
      }),
    );

    expect(nodeById(nodes, "service:metrics").data).toMatchObject({ referenced: false });
    expect(connections(edges)).not.toContain("router:web->service:metrics");
  });

  it("keeps a declared middleware that no router references, marked unreferenced", () => {
    const { nodes } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web", service: "api" }],
        services: [{ name: "api" }],
        middlewares: [{ name: "auth", type: "basicauth" }],
      }),
    );

    expect(nodeById(nodes, "middleware:auth").data).toMatchObject({ referenced: false });
  });

  it("orders the service column by first use, unreferenced last", () => {
    const { nodes } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [{ name: "web", service: "api" }, { name: "legacy" }],
        services: [{ name: "metrics" }, { name: "api" }],
      }),
    );

    expect(nodes.filter((node) => node.type === "traefikService").map((node) => node.id)).toEqual([
      "service:api",
      "service:unresolved:legacy",
      "service:metrics",
    ]);
  });

  it("emits one edge when two routers share the same chain step", () => {
    const { edges } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [
          { name: "web", middlewares: ["auth"], service: "api" },
          { name: "admin", middlewares: ["auth"], service: "api" },
        ],
        services: [{ name: "api" }],
        middlewares: [{ name: "auth", type: "basicauth" }],
      }),
    );

    expect(
      connections(edges).filter((pair) => pair === "middleware:auth->service:api"),
    ).toHaveLength(1);
  });

  it("carries the router's rule, priority and TLS onto its node", () => {
    const { nodes } = traefikIntegrationToReactFlow(
      makeIntegration({
        routers: [
          {
            name: "web",
            rule: "Host(`example.com`)",
            priority: 10,
            tls: { certResolver: "le" },
            service: "api",
          },
        ],
        services: [{ name: "api" }],
      }),
    );

    expect(nodeById(nodes, "router:web").data).toMatchObject({
      name: "web",
      rule: "Host(`example.com`)",
      priority: 10,
      tls: { certResolver: "le" },
    });
  });
});
