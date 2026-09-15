import { EntrypointNode, MiddlewareNode, RouterNode, ServiceNode } from "./TraefikGraphNodes";
import type { TraefikIntegration } from "@/api/types";
import { MeasuredGraph } from "@/components/graph/MeasuredGraph";
import { useNodeSelection } from "@/components/graph/useNodeSelection";
import type { LayerConstraints } from "@/lib/graphLayout";
import { traefikIntegrationToReactFlow } from "@/lib/traefikGraph";
import { useMemo } from "react";

const nodeTypes = {
  traefikEntrypoint: EntrypointNode,
  traefikRouter: RouterNode,
  traefikMiddleware: MiddlewareNode,
  traefikService: ServiceNode,
};

// Traffic enters at an entrypoint and lands on a service, so those two keep
// their own ends of the graph whatever the chains between them look like.
const layerConstraints: LayerConstraints = {
  traefikEntrypoint: "FIRST",
  traefikService: "LAST",
};

export default function TraefikGraph({ integration }: { integration: TraefikIntegration }) {
  const graph = useMemo(() => traefikIntegrationToReactFlow(integration), [integration]);
  const selection = useNodeSelection();

  return (
    <MeasuredGraph
      graph={graph}
      nodeTypes={nodeTypes}
      label="Traefik routing"
      layerConstraints={layerConstraints}
      selection={selection}
    />
  );
}
