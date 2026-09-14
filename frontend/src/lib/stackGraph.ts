import type { Service, StackDetail } from "../api/types";
import { stripStackPrefix } from "./parseStackLabels";
import { MarkerType, type Edge, type Node } from "@xyflow/react";

export type NetworkNodeData = {
  name: string;
  href: string;
  driver?: string | undefined;
  scope?: string | undefined;
  external: boolean;
  referenced: boolean;
  aliases: { service: string; names: string[] }[];
};

export type ServiceNodeData = {
  name: string;
  href: string;
  image?: string | undefined;
  mode: string;
  replicas?: number | undefined;
  tasks?: TaskCount | undefined;
};

/** What the page's own task poll found, absent until the first answer lands. */
export type TaskCount = { running: number; desired: number };

export type MountNodeData = {
  name: string;
  href: string;
  detail?: string | undefined;
  referenced: boolean;
  mountedBy: { service: string; path: string }[];
};

const origin = { x: 0, y: 0 };

/** The image reference without its digest, which is never the interesting half. */
function shortImage(image: string | undefined): string | undefined {
  return image?.split("@")[0];
}

function serviceMode(service: Service): { mode: string; replicas?: number | undefined } {
  return service.Spec.Mode.Global
    ? { mode: "global" }
    : { mode: "replicated", replicas: service.Spec.Mode.Replicated?.Replicas };
}

/**
 * Read a stack as the graph of what it holds: the networks its services attach
 * to, and the configs, secrets and volumes they mount. A resource the stack
 * does not hold becomes a node marked external, so the attachment is visible.
 */
export function stackToReactFlow(
  stack: StackDetail,
  tasks: Record<string, TaskCount> = {},
): { nodes: Node[]; edges: Edge[] } {
  // Every name here is prefixed with the stack whose page this is, which the
  // page title already says.
  const bare = (name: string) => stripStackPrefix(name, stack.name);

  const networkNodes = new Map<string, Node>();
  const serviceNodes: Node[] = [];
  const mountNodes = new Map<string, Node>();
  const edges: Edge[] = [];

  function connect(source: string, target: string) {
    edges.push({
      id: `${source}->${target}`,
      source,
      target,
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
    });
  }

  function mountNode(id: string, type: string, data: MountNodeData): Node {
    return { id, type, position: origin, data };
  }

  // An edge says a service reaches a resource. Where it lands in the container,
  // and what a service answers to on a network, are what the arrow cannot say.
  function mountedAt(id: string, service: string, path: string) {
    (mountNodes.get(id)?.data as MountNodeData | undefined)?.mountedBy.push({ service, path });
  }

  function answersTo(id: string, service: string, names: string[]) {
    (networkNodes.get(id)?.data as NetworkNodeData | undefined)?.aliases.push({ service, names });
  }

  /** Marks a node referenced, adding it first when the stack does not hold it. */
  function reference(nodes: Map<string, Node>, id: string, make: () => Node): string {
    const existing = nodes.get(id);

    if (existing) {
      (existing.data as { referenced: boolean }).referenced = true;
    } else {
      nodes.set(id, make());
    }

    return id;
  }

  for (const network of stack.networks) {
    networkNodes.set(`network:${network.Id}`, {
      id: `network:${network.Id}`,
      type: "stackNetwork",
      position: origin,
      data: {
        name: bare(network.Name),
        href: `/networks/${network.Id}`,
        driver: network.Driver,
        scope: network.Scope,
        external: false,
        referenced: false,
        aliases: [],
      } satisfies NetworkNodeData,
    });
  }

  for (const config of stack.configs) {
    mountNodes.set(
      `config:${config.ID}`,
      mountNode(`config:${config.ID}`, "stackConfig", {
        name: bare(config.Spec.Name),
        href: `/configs/${config.ID}`,
        referenced: false,
        mountedBy: [],
      }),
    );
  }

  for (const secret of stack.secrets) {
    mountNodes.set(
      `secret:${secret.ID}`,
      mountNode(`secret:${secret.ID}`, "stackSecret", {
        name: bare(secret.Spec.Name),
        href: `/secrets/${secret.ID}`,
        referenced: false,
        mountedBy: [],
      }),
    );
  }

  for (const volume of stack.volumes) {
    mountNodes.set(
      `volume:${volume.Name}`,
      mountNode(`volume:${volume.Name}`, "stackVolume", {
        name: bare(volume.Name),
        href: `/volumes/${volume.Name}`,
        detail: volume.Driver,
        referenced: false,
        mountedBy: [],
      }),
    );
  }

  function referenceMount(id: string, type: string, name: string, href: string): string {
    return reference(mountNodes, id, () =>
      mountNode(id, type, { name: bare(name), href, referenced: true, mountedBy: [] }),
    );
  }

  for (const service of stack.services) {
    const serviceId = `service:${service.ID}`;
    const name = bare(service.Spec.Name);
    const container = service.Spec.TaskTemplate?.ContainerSpec;

    serviceNodes.push({
      id: serviceId,
      type: "stackService",
      position: origin,
      data: {
        name,
        href: `/services/${service.ID}`,
        image: shortImage(container?.Image),
        ...serviceMode(service),
        tasks: tasks[service.ID],
      } satisfies ServiceNodeData,
    });

    for (const { Target, Aliases } of service.Spec.TaskTemplate?.Networks ?? []) {
      if (!Target) {
        continue;
      }

      const networkId = `network:${Target}`;

      reference(networkNodes, networkId, () => ({
        id: networkId,
        type: "stackNetwork",
        position: origin,
        data: {
          name: Target.slice(0, 12),
          href: `/networks/${Target}`,
          external: true,
          referenced: true,
          aliases: [],
        } satisfies NetworkNodeData,
      }));

      connect(networkId, serviceId);

      if (Aliases?.length) {
        answersTo(networkId, name, Aliases);
      }
    }

    for (const { ConfigID, ConfigName, File } of container?.Configs ?? []) {
      const id = referenceMount(
        `config:${ConfigID}`,
        "stackConfig",
        ConfigName,
        `/configs/${ConfigID}`,
      );

      connect(serviceId, id);
      mountedAt(id, name, File?.Name ?? `/${ConfigName}`);
    }

    for (const { SecretID, SecretName, File } of container?.Secrets ?? []) {
      const id = referenceMount(
        `secret:${SecretID}`,
        "stackSecret",
        SecretName,
        `/secrets/${SecretID}`,
      );

      connect(serviceId, id);
      mountedAt(id, name, File?.Name ?? `/run/secrets/${SecretName}`);
    }

    for (const { Type, Source, Target, ReadOnly } of container?.Mounts ?? []) {
      if (Type !== "volume" || !Source) {
        continue;
      }

      const id = referenceMount(`volume:${Source}`, "stackVolume", Source, `/volumes/${Source}`);

      connect(serviceId, id);

      if (Target) {
        mountedAt(id, name, ReadOnly ? `${Target} (read-only)` : Target);
      }
    }
  }

  return {
    nodes: [...networkNodes.values(), ...serviceNodes, ...mountNodes.values()],
    edges,
  };
}
