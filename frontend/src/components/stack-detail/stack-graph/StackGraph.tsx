import { ConfigNode, NetworkNode, SecretNode, ServiceNode, VolumeNode } from "./StackGraphNodes";
import type { StackDetail } from "@/api/types";
import { MeasuredGraph } from "@/components/graph/MeasuredGraph";
import type { LayerConstraints } from "@/lib/graphLayout";
import { stackToReactFlow } from "@/lib/stackGraph";
import { useMemo } from "react";

const nodeTypes = {
  stackNetwork: NetworkNode,
  stackService: ServiceNode,
  stackConfig: ConfigNode,
  stackSecret: SecretNode,
  stackVolume: VolumeNode,
};

// Services attach to networks and mount everything else, so the reading runs
// network → service → what it carries.
const layerConstraints: LayerConstraints = {
  stackNetwork: "FIRST",
  stackConfig: "LAST",
  stackSecret: "LAST",
  stackVolume: "LAST",
};

export default function StackGraph({ stack }: { stack: StackDetail }) {
  const graph = useMemo(() => stackToReactFlow(stack), [stack]);

  // Only the shape needs to survive a re-layout: a service's box is a fixed
  // width and a fixed three lines whatever its image or replica count say.
  const shape = useMemo(
    () => [...graph.nodes.map(({ id }) => id), ...graph.edges.map(({ id }) => id)].join("|"),
    [graph],
  );

  return (
    <MeasuredGraph
      key={shape}
      graph={graph}
      nodeTypes={nodeTypes}
      layerConstraints={layerConstraints}
    />
  );
}
