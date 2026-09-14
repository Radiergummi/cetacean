import { mountNodeType, NetworkNode, ServiceNode } from "./StackGraphNodes";
import type { StackDetail } from "@/api/types";
import { MeasuredGraph } from "@/components/graph/MeasuredGraph";
import type { LayerConstraints } from "@/lib/graphLayout";
import { stackToReactFlow, type TaskCount } from "@/lib/stackGraph";
import { FileText, HardDrive, KeyRound } from "lucide-react";
import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";

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

const nodeParam = "node";

// Services attach to networks and mount everything else, so the reading runs
// network → service → what it carries.
const layerConstraints: LayerConstraints = {
  stackNetwork: "FIRST",
  stackConfig: "LAST",
  stackSecret: "LAST",
  stackVolume: "LAST",
};

export default function StackGraph({
  stack,
  taskCounts,
}: {
  stack: StackDetail;
  taskCounts: Record<string, TaskCount>;
}) {
  const graph = useMemo(() => stackToReactFlow(stack, taskCounts), [stack, taskCounts]);
  const [params, setParams] = useSearchParams();

  // Replaces rather than pushes: tabbing across the graph is not a trail of
  // pages to walk back through.
  const select = useCallback(
    (id: string | null) => {
      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          if (id) {
            next.set(nodeParam, id);
          } else {
            next.delete(nodeParam);
          }

          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  return (
    <MeasuredGraph
      graph={graph}
      nodeTypes={nodeTypes}
      label={`Topology of stack ${stack.name}`}
      layerConstraints={layerConstraints}
      selection={params.get(nodeParam)}
      onSelect={select}
    />
  );
}
