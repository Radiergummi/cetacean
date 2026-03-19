import { useHighlight } from "./HighlightContext";
import { EdgeLabelRenderer, type EdgeProps, getSmoothStepPath } from "@xyflow/react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

type NetworkInfo = {
  id: string;
  name: string;
  driver: string;
  scope: string;
  stack?: string;
  color?: string;
};

type NetworkEdgeData = {
  networks: NetworkInfo[];
  bendPoints?: Array<{ x: number; y: number }>;
  sourceAliases?: string[];
  targetAliases?: string[];
};

/** Snap points so each segment is strictly horizontal or vertical */
function snapToOrthogonal(
  points: Array<{ x: number; y: number }>,
): Array<{ x: number; y: number }> {
  if (points.length < 2) {
    return points;
  }

  const result = [{ ...points[0] }];

  for (let index = 1; index < points.length; index++) {
    const previous = result[result.length - 1];
    const current = { ...points[index] };
    const dx = Math.abs(current.x - previous.x);
    const dy = Math.abs(current.y - previous.y);

    if (dx >= dy) {
      current.y = previous.y;
    } else {
      current.x = previous.x;
    }

    result.push(current);
  }
  return result;
}

/** Build an SVG path from orthogonal bend points with rounded corners */
function buildOrthogonalPath(points: Array<{ x: number; y: number }>, radius = 6): string {
  if (points.length < 2) {
    return "";
  }

  if (points.length === 2) {
    return `M ${points[0].x} ${points[0].y} L ${points[1].x} ${points[1].y}`;
  }

  let d = `M ${points[0].x} ${points[0].y}`;

  for (let index = 1; index < points.length - 1; index++) {
    const prev = points[index - 1];
    const curr = points[index];
    const next = points[index + 1];

    const dPrev = Math.max(Math.abs(curr.x - prev.x), Math.abs(curr.y - prev.y));
    const dNext = Math.max(Math.abs(next.x - curr.x), Math.abs(next.y - curr.y));
    const r = Math.min(radius, dPrev / 2, dNext / 2);

    const dx1 = Math.sign(curr.x - prev.x);
    const dy1 = Math.sign(curr.y - prev.y);
    const dx2 = Math.sign(next.x - curr.x);
    const dy2 = Math.sign(next.y - curr.y);

    const ax = curr.x - dx1 * r;
    const ay = curr.y - dy1 * r;
    const bx = curr.x + dx2 * r;
    const by = curr.y + dy2 * r;

    d += ` L ${ax} ${ay}`;
    d += ` Q ${curr.x} ${curr.y} ${bx} ${by}`;
  }

  const last = points[points.length - 1];
  d += ` L ${last.x} ${last.y}`;

  return d;
}

export default function NetworkEdge({
  id,
  source,
  target,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
}: EdgeProps & { data: NetworkEdgeData }) {
  const [hovered, setHovered] = useState(false);
  const navigate = useNavigate();
  const { hoveredId } = useHighlight();

  const highlighted = hoveredId != null && (source === hoveredId || target === hoveredId);
  const dimmed = hoveredId != null && !highlighted;

  const isStackNetwork = data.networks.some(({ color: colorCode }) => colorCode != null);
  const color = isStackNetwork
    ? data.networks.find((n) => n.color)!.color!
    : "var(--color-muted-foreground)";

  const baseWidth = isStackNetwork ? 1.5 : 1;
  const strokeWidth = highlighted ? 2.5 : hovered ? 2 : baseWidth;

  let edgePath: string;
  let labelX: number;
  let labelY: number;

  if (data.bendPoints && data.bendPoints.length >= 2) {
    const snapped = snapToOrthogonal(data.bendPoints);
    edgePath = buildOrthogonalPath(snapped, 8);
    const mid = snapped[Math.floor(snapped.length / 2)];
    labelX = mid.x;
    labelY = mid.y;
  } else {
    const result = getSmoothStepPath({
      sourceX,
      sourceY,
      targetX,
      targetY,
      sourcePosition,
      targetPosition,
      borderRadius: 8,
    });
    edgePath = result[0];
    labelX = result[1];
    labelY = result[2];
  }

  return (
    <>
      {/* Visible path */}
      <path
        id={id}
        d={edgePath}
        data-highlighted={highlighted || undefined}
        data-dimmed={dimmed || undefined}
        style={{
          stroke: color,
          strokeWidth,
        }}
        fill="none"
        className="react-flow__edge-path data-highlighted:topology-edge-flow transition-all data-dimmed:opacity-15 data-highlighted:opacity-100 data-highlighted:[stroke-dasharray:6_4]"
      />
      {/* Hover target (wide invisible path) */}
      <path
        d={edgePath}
        strokeWidth={16}
        stroke="transparent"
        fill="none"
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      <EdgeLabelRenderer>
        {data.sourceAliases && !dimmed && (
          <div
            className="pointer-events-none absolute rounded bg-muted/90 px-1.5 py-0.5 font-mono text-[10px] whitespace-nowrap text-muted-foreground"
            style={{
              transform: `translate(-100%, -50%) translate(${sourceX - 4}px, ${sourceY}px)`,
            }}
          >
            {data.sourceAliases.join(", ")}
          </div>
        )}
        {data.targetAliases && !dimmed && (
          <div
            className="pointer-events-none absolute rounded bg-muted/90 px-1.5 py-0.5 font-mono text-[10px] whitespace-nowrap text-muted-foreground"
            style={{
              transform: `translate(0%, -50%) translate(${targetX + 4}px, ${targetY}px)`,
            }}
          >
            {data.targetAliases.join(", ")}
          </div>
        )}
        {hovered && (
          <div
            className="absolute rounded-lg border bg-popover px-2.5 py-1.5 text-xs shadow-md"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
              zIndex: 1000,
            }}
          >
            {data.networks.map((net) => (
              <div
                key={net.id}
                className="flex cursor-pointer items-center gap-1.5 py-0.5 hover:underline"
                onMouseDown={(e) => {
                  e.stopPropagation();
                  navigate(`/networks/${net.id}`);
                }}
              >
                <span
                  className="inline-block size-2 shrink-0 rounded-full"
                  style={{ backgroundColor: net.color ?? "var(--color-muted-foreground)" }}
                />
                <span className="font-medium">{net.name}</span>
                <span className="text-muted-foreground">
                  {net.driver} · {net.scope}
                </span>
              </div>
            ))}
          </div>
        )}
      </EdgeLabelRenderer>
    </>
  );
}
