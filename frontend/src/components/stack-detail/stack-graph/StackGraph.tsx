import { mountNodeType, NetworkNode, ServiceNode } from "./StackGraphNodes";
import type { StackDetail } from "@/api/types";
import { MeasuredGraph } from "@/components/graph/MeasuredGraph";
import type { LayerConstraints } from "@/lib/graphLayout";
import { stackToReactFlow } from "@/lib/stackGraph";
import { FileText, HardDrive, KeyRound } from "lucide-react";
import { useMemo } from "react";

const nodeTypes = {
  stackNetwork: NetworkNode,
  stackService: ServiceNode,
  stackConfig: mountNodeType(
    "Config",
    <FileText className="size-3 shrink-0 text-blue-600 dark:text-blue-400" />,
  ),
  stackSecret: mountNodeType(
    "Secret",
    <KeyRound className="size-3 shrink-0 text-amber-600 dark:text-amber-400" />,
  ),
  stackVolume: mountNodeType(
    "Volume",
    <HardDrive className="size-3 shrink-0 text-purple-600 dark:text-purple-400" />,
  ),
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

  return (
    <MeasuredGraph
      graph={graph}
      nodeTypes={nodeTypes}
      layerConstraints={layerConstraints}
    />
  );
}
