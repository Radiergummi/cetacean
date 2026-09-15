import { mountNodeType, NetworkNode, ServiceNode } from "./StackGraphNodes";
import type { StackDetail } from "@/api/types";
import { GraphLegend, dashedMark, mutedMark } from "@/components/graph/GraphLegend";
import { MeasuredGraph } from "@/components/graph/MeasuredGraph";
import { useNodeSelection } from "@/components/graph/useNodeSelection";
import type { LayerConstraints } from "@/lib/graphLayout";
import { stackToReactFlow, type TaskCount } from "@/lib/stackGraph";
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

const legend = (
  <GraphLegend
    entries={[
      { mark: <FileText className="size-3 text-blue-600 dark:text-blue-400" />, label: "Config" },
      { mark: <KeyRound className="size-3 text-amber-600 dark:text-amber-400" />, label: "Secret" },
      {
        mark: <HardDrive className="size-3 text-purple-600 dark:text-purple-400" />,
        label: "Volume",
      },
      { mark: dashedMark, label: "Not in this stack" },
      { mark: mutedMark, label: "Unreferenced" },
    ]}
  />
);

export default function StackGraph({
  stack,
  taskCounts,
}: {
  stack: StackDetail;
  taskCounts: Record<string, TaskCount>;
}) {
  const graph = useMemo(() => stackToReactFlow(stack, taskCounts), [stack, taskCounts]);
  const selection = useNodeSelection();

  return (
    <MeasuredGraph
      graph={graph}
      nodeTypes={nodeTypes}
      label={`Topology of stack ${stack.name}`}
      layerConstraints={layerConstraints}
      legend={legend}
      selection={selection}
    />
  );
}
