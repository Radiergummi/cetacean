import { useState } from "react";
import { EdgeLabelRenderer, type EdgeProps } from "@xyflow/react";

type NetworkEdgeData = {
  color: string;
  networkName: string;
  networkDriver: string;
  parallelIndex?: number;
  parallelCount?: number;
  bendPoints?: Array<{ x: number; y: number }>;
};

function buildOrthogonalPath(points: Array<{ x: number; y: number }>): string {
  if (points.length < 2) return "";
  let d = `M ${points[0].x} ${points[0].y}`;
  for (let i = 1; i < points.length; i++) {
    d += ` L ${points[i].x} ${points[i].y}`;
  }
  return d;
}

export default function NetworkEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  data,
}: EdgeProps & { data: NetworkEdgeData }) {
  const [hovered, setHovered] = useState(false);

  // Use ELK bend points if available, fall back to straight line
  const points = data.bendPoints && data.bendPoints.length >= 2
    ? data.bendPoints
    : [{ x: sourceX, y: sourceY }, { x: targetX, y: targetY }];

  // Offset parallel edges slightly so multiple networks between same pair are visible
  const offset = data.parallelCount && data.parallelCount > 1
    ? ((data.parallelIndex ?? 0) - ((data.parallelCount - 1) / 2)) * 6
    : 0;

  const offsetPoints = offset !== 0
    ? points.map((p) => ({ x: p.x, y: p.y + offset }))
    : points;

  const edgePath = buildOrthogonalPath(offsetPoints);

  // Label position at midpoint
  const mid = offsetPoints[Math.floor(offsetPoints.length / 2)];

  return (
    <>
      <path
        id={id}
        d={edgePath}
        stroke={data.color}
        strokeWidth={hovered ? 3 : 2}
        strokeOpacity={hovered ? 1 : 0.6}
        fill="none"
        className="react-flow__edge-path transition-all"
      />
      <path
        d={edgePath}
        strokeWidth={16}
        stroke="transparent"
        fill="none"
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      {hovered && mid && (
        <EdgeLabelRenderer>
          <div
            className="absolute rounded bg-popover border shadow-sm px-2 py-1 text-xs pointer-events-none"
            style={{
              transform: `translate(-50%, -50%) translate(${mid.x}px, ${mid.y}px)`,
              zIndex: 1000,
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
