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
};

export type ServiceNodeData = {
  name: string;
  href: string;
  image?: string | undefined;
  mode: string;
  replicas?: number | undefined;
};

export type MountNodeData = {
  name: string;
  href: string;
  detail?: string | undefined;
  referenced: boolean;
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
export function stackToReactFlow(stack: StackDetail): { nodes: Node[]; edges: Edge[] } {
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
      }),
    );
  }

  function referenceMount(id: string, type: string, name: string, href: string): string {
    return reference(mountNodes, id, () =>
      mountNode(id, type, { name: bare(name), href, referenced: true }),
    );
  }

  for (const service of stack.services) {
    const serviceId = `service:${service.ID}`;
    const container = service.Spec.TaskTemplate?.ContainerSpec;

    serviceNodes.push({
      id: serviceId,
      type: "stackService",
      position: origin,
      data: {
        name: bare(service.Spec.Name),
        href: `/services/${service.ID}`,
        image: shortImage(container?.Image),
        ...serviceMode(service),
      } satisfies ServiceNodeData,
    });

    for (const { Target } of service.Spec.TaskTemplate?.Networks ?? []) {
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
        } satisfies NetworkNodeData,
      }));

      connect(networkId, serviceId);
    }

    for (const { ConfigID, ConfigName } of container?.Configs ?? []) {
      connect(
        serviceId,
        referenceMount(`config:${ConfigID}`, "stackConfig", ConfigName, `/configs/${ConfigID}`),
      );
    }

    for (const { SecretID, SecretName } of container?.Secrets ?? []) {
      connect(
        serviceId,
        referenceMount(`secret:${SecretID}`, "stackSecret", SecretName, `/secrets/${SecretID}`),
      );
    }

    for (const { Type, Source } of container?.Mounts ?? []) {
      if (Type !== "volume" || !Source) {
        continue;
      }

      connect(
        serviceId,
        referenceMount(`volume:${Source}`, "stackVolume", Source, `/volumes/${Source}`),
      );
    }
  }

  return {
    nodes: [...networkNodes.values(), ...serviceNodes, ...mountNodes.values()],
    edges,
  };
}
