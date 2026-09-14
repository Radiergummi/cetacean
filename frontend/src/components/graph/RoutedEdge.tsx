import type { RoutedEdgeData } from "@/lib/graphLayout";
import { BaseEdge, type EdgeProps } from "@xyflow/react";

type Point = { x: number; y: number };

const minimumReach = 16;

/**
 * Control points for the segment starting at `index`, Catmull-Rom style: an
 * interior waypoint takes its tangent from its neighbours, so the segments
 * join smoothly, while the two ends leave and enter their node horizontally.
 */
function controlPoints(points: Point[], index: number): [Point, Point] {
  const before = points[index - 1];
  const start = points[index]!;
  const end = points[index + 1]!;
  const after = points[index + 2];
  const reach = Math.max(minimumReach, Math.abs(end.x - start.x) / 2);

  return [
    before
      ? { x: start.x + (end.x - before.x) / 6, y: start.y + (end.y - before.y) / 6 }
      : { x: start.x + reach, y: start.y },
    after
      ? { x: end.x - (after.x - start.x) / 6, y: end.y - (after.y - start.y) / 6 }
      : { x: end.x - reach, y: end.y },
  ];
}

function smoothPath(points: Point[]): string {
  const first = points[0];

  if (!first || points.length < 2) {
    return "";
  }

  let path = `M ${first.x},${first.y}`;

  for (let index = 0; index < points.length - 1; index++) {
    const [from, to] = controlPoints(points, index);
    const end = points[index + 1]!;

    path += ` C ${from.x},${from.y} ${to.x},${to.y} ${end.x},${end.y}`;
  }

  return path;
}

/** Draws ELK's route as one smooth curve rather than a run of straight segments. */
export function RoutedEdge({ markerEnd, style, data }: EdgeProps) {
  const { points } = data as RoutedEdgeData;

  return (
    <BaseEdge
      path={smoothPath(points)}
      style={style}
      {...(markerEnd == null ? {} : { markerEnd })}
    />
  );
}
