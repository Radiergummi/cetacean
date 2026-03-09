import { useState } from "react";
import { getSmoothStepPath, EdgeLabelRenderer, type EdgeProps } from "@xyflow/react";

type NetworkEdgeData = {
  color: string;
  networkName: string;
  networkDriver: string;
};

export default function NetworkEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
}: EdgeProps & { data: NetworkEdgeData }) {
  const [hovered, setHovered] = useState(false);

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
  });

  return (
    <>
      <path
        id={id}
        d={edgePath}
        stroke={data.color}
        strokeWidth={2}
        fill="none"
        className="react-flow__edge-path"
      />
      <path
        d={edgePath}
        strokeWidth={16}
        stroke="transparent"
        fill="none"
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      {hovered && (
        <EdgeLabelRenderer>
          <div
            className="absolute rounded bg-popover border shadow-sm px-2 py-1 text-xs pointer-events-none"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            }}
          >
            <div className="font-medium">{data.networkName}</div>
            <div className="text-muted-foreground">{data.networkDriver}</div>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
