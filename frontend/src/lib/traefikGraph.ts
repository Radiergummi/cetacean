import type {
  TraefikIntegration,
  TraefikRouter,
  TraefikService,
  TraefikTLSDomain,
} from "../api/types";
import { MarkerType, type Edge, type Node } from "@xyflow/react";

export type EntrypointNodeData = {
  name: string;
};

export type RouterNodeData = {
  name: string;
  rule?: string | undefined;
  priority?: number | undefined;
  tls?:
    | {
        certResolver?: string | undefined;
        domains?: TraefikTLSDomain[] | undefined;
        options?: string | undefined;
      }
    | undefined;
};

export type MiddlewareNodeData = {
  name: string;
  type?: string | undefined;
  config?: Record<string, string> | undefined;
  external: boolean;
  referenced: boolean;
};

export type ServiceNodeData = {
  name: string;
  port?: number | undefined;
  scheme?: string | undefined;
  origin: "declared" | "external" | "unresolved";
  implicit: boolean;
  referenced: boolean;
};

const origin = { x: 0, y: 0 };

/**
 * Read one service's Traefik labels as the graph they describe: entrypoints →
 * routers → the middleware chain → services. A reference the labels do not
 * define becomes a node marked external, because a missing arrow reads as "no
 * middleware" rather than "defined elsewhere". Positions come from ELK later.
 */
export function traefikIntegrationToReactFlow(integration: TraefikIntegration): {
  nodes: Node[];
  edges: Edge[];
} {
  const routers = integration.routers ?? [];
  const declaredServices = integration.services ?? [];
  const declaredMiddlewares = integration.middlewares ?? [];

  const entrypointNodes = new Map<string, Node>();
  const routerNodes: Node[] = [];
  const middlewareNodes = new Map<string, Node>();
  const serviceNodes = new Map<string, Node>();
  const edges = new Map<string, Edge>();

  function connect(source: string, target: string) {
    const id = `${source}->${target}`;

    if (!edges.has(id)) {
      edges.set(id, {
        id,
        source,
        target,
        markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
      });
    }
  }

  function referenceMiddleware(name: string, referenced = true): string {
    const id = `middleware:${name}`;

    if (!middlewareNodes.has(id)) {
      const declared = declaredMiddlewares.find((middleware) => middleware.name === name);

      middlewareNodes.set(id, {
        id,
        type: "traefikMiddleware",
        position: origin,
        data: {
          name,
          type: declared?.type,
          config: declared?.config,
          external: declared == null,
          referenced,
        } satisfies MiddlewareNodeData,
      });
    }

    return id;
  }

  function declaredServiceNode(
    service: TraefikService,
    implicit: boolean,
    referenced = true,
  ): string {
    const id = `service:${service.name}`;
    const existing = serviceNodes.get(id);

    if (existing) {
      (existing.data as ServiceNodeData).implicit ||= implicit;

      return id;
    }

    serviceNodes.set(id, {
      id,
      type: "traefikService",
      position: origin,
      data: {
        name: service.name,
        port: service.port,
        scheme: service.scheme,
        origin: "declared",
        implicit,
        referenced,
      } satisfies ServiceNodeData,
    });

    return id;
  }

  function referenceService(router: TraefikRouter): string {
    if (router.service) {
      const declared = declaredServices.find(({ name }) => name === router.service);

      if (declared) {
        return declaredServiceNode(declared, false);
      }

      const id = `service:${router.service}`;

      serviceNodes.set(id, {
        id,
        type: "traefikService",
        position: origin,
        data: {
          name: router.service,
          origin: "external",
          implicit: false,
          referenced: true,
        } satisfies ServiceNodeData,
      });

      return id;
    }

    // Traefik binds a router that names no service to the container's single
    // service; with none or several declared the binding cannot be read off
    // these labels at all.
    if (declaredServices.length === 1) {
      return declaredServiceNode(declaredServices[0]!, true);
    }

    const id = `service:unresolved:${router.name}`;

    serviceNodes.set(id, {
      id,
      type: "traefikService",
      position: origin,
      data: {
        name: router.name,
        origin: "unresolved",
        implicit: false,
        referenced: true,
      } satisfies ServiceNodeData,
    });

    return id;
  }

  for (const router of routers) {
    const routerId = `router:${router.name}`;

    routerNodes.push({
      id: routerId,
      type: "traefikRouter",
      position: origin,
      data: {
        name: router.name,
        rule: router.rule,
        priority: router.priority,
        tls: router.tls,
      } satisfies RouterNodeData,
    });

    for (const entrypoint of router.entrypoints ?? []) {
      const entrypointId = `entrypoint:${entrypoint}`;

      if (!entrypointNodes.has(entrypointId)) {
        entrypointNodes.set(entrypointId, {
          id: entrypointId,
          type: "traefikEntrypoint",
          position: origin,
          data: { name: entrypoint } satisfies EntrypointNodeData,
        });
      }

      connect(entrypointId, routerId);
    }

    const chain = (router.middlewares ?? []).map((name) => referenceMiddleware(name));
    const serviceId = referenceService(router);

    let previous = routerId;

    for (const middlewareId of chain) {
      connect(previous, middlewareId);
      previous = middlewareId;
    }

    connect(previous, serviceId);
  }

  for (const middleware of declaredMiddlewares) {
    referenceMiddleware(middleware.name, false);
  }

  for (const service of declaredServices) {
    declaredServiceNode(service, false, false);
  }

  return {
    nodes: [
      ...entrypointNodes.values(),
      ...routerNodes,
      ...middlewareNodes.values(),
      ...serviceNodes.values(),
    ],
    edges: [...edges.values()],
  };
}
