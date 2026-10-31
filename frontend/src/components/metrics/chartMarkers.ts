import type { TooltipMarkers } from "./ChartTooltipOverlay";
import { formatDateTime } from "@/lib/format";
import { describeChange, describeGap, type TimelineItem } from "@/lib/timeline";

type PointItem = Extract<TimelineItem, { shape: "point" }>;
type RangeItem = Extract<TimelineItem, { shape: "range" }>;

/**
 * Points closer than this many pixels draw as one marker, and the pointer
 * finds a marker this close to it.
 */
export const markerRadius = 6;

/**
 * How many of a marker's changes its tooltip lists before it counts the rest.
 */
const tooltipChanges = 5;

/**
 * The spacing of a gap's hatching, in pixels.
 */
const hatchSpacing = 6;

export interface MarkerCluster {
  x: number;
  items: PointItem[];
}

export interface MarkerBand {
  left: number;
  right: number;
  item: RangeItem;
}

export interface MarkerLayout {
  clusters: MarkerCluster[];
  bands: MarkerBand[];
}

/**
 * Where a time falls on a chart whose x-axis is the index of its samples,
 * interpolated between the two around it. A time within one step outside the
 * samples is pinned to the nearest end: the scrape had not caught up with it.
 */
export function fractionalIndex(timestamps: number[], time: number): number | null {
  const first = timestamps[0];
  const last = timestamps.at(-1);

  if (first === undefined || last === undefined) {
    return null;
  }

  if (time <= first) {
    const step = (timestamps[1] ?? first) - first;

    return time >= first - step ? 0 : null;
  }

  if (time >= last) {
    const step = timestamps.length > 1 ? last - (timestamps.at(-2) ?? last) : 0;

    return time <= last + step ? timestamps.length - 1 : null;
  }

  let low = 0;
  let high = timestamps.length - 1;

  while (high - low > 1) {
    const middle = (low + high) >> 1;

    if ((timestamps[middle] ?? 0) <= time) {
      low = middle;
    } else {
      high = middle;
    }
  }

  const start = timestamps[low] ?? first;
  const end = timestamps[high] ?? last;

  return end > start ? low + (time - start) / (end - start) : low;
}

/**
 * Places timeline items in pixels. Points outside the samples are dropped, and
 * ranges are clipped to them: a gap that began before the window still covers
 * its start.
 */
export function layoutMarkers(
  items: TimelineItem[],
  timestamps: number[],
  pixelFor: (index: number) => number,
): MarkerLayout {
  const first = timestamps[0];
  const last = timestamps.at(-1);
  const points: { x: number; item: PointItem }[] = [];
  const bands: MarkerBand[] = [];

  if (first === undefined || last === undefined) {
    return { clusters: [], bands };
  }

  for (const item of items) {
    if (item.shape === "point") {
      const index = fractionalIndex(timestamps, item.at);

      if (index !== null) {
        points.push({ x: pixelFor(index), item });
      }

      continue;
    }

    if (item.to <= first || item.from >= last) {
      continue;
    }

    const left = fractionalIndex(timestamps, Math.max(item.from, first)) ?? 0;
    const right = fractionalIndex(timestamps, Math.min(item.to, last)) ?? timestamps.length - 1;

    bands.push({ left: pixelFor(left), right: pixelFor(right), item });
  }

  points.sort((one, other) => one.x - other.x);

  const clusters: MarkerCluster[] = [];
  let start = Number.NEGATIVE_INFINITY;

  for (const { x, item } of points) {
    const cluster = clusters.at(-1);

    if (cluster && x - start < markerRadius) {
      cluster.items.push(item);
      cluster.x = start + (x - start) / 2;

      continue;
    }

    start = x;
    clusters.push({ x, items: [item] });
  }

  return { clusters, bands };
}

/**
 * The marker and the band under the pointer, if any.
 */
export function markersAt(
  layout: MarkerLayout | null,
  x: number,
): { cluster: MarkerCluster | undefined; band: MarkerBand | undefined } {
  let cluster: MarkerCluster | undefined;

  for (const candidate of layout?.clusters ?? []) {
    const distance = Math.abs(candidate.x - x);

    if (distance <= markerRadius && (!cluster || distance < Math.abs(cluster.x - x))) {
      cluster = candidate;
    }
  }

  const band = layout?.bands.find(({ left, right }) => x >= left && x <= right);

  return { cluster, band };
}

/**
 * Draws bands as hatched shading and clusters as vertical lines with a cap;
 * a cluster of several gets a heavier one.
 */
export function drawMarkers(
  context: CanvasRenderingContext2D,
  area: { top: number; bottom: number; left: number; right: number },
  { clusters, bands }: MarkerLayout,
  colors: { marker: string; gap: string },
) {
  const height = area.bottom - area.top;

  for (const { left, right } of bands) {
    const clippedLeft = Math.max(left, area.left);
    const clippedRight = Math.min(right, area.right);

    context.save();
    context.beginPath();
    context.rect(clippedLeft, area.top, clippedRight - clippedLeft, height);
    context.clip();
    context.fillStyle = colors.gap;
    context.fill();
    context.strokeStyle = colors.gap;
    context.lineWidth = 1;
    context.beginPath();

    for (let x = clippedLeft - height; x < clippedRight; x += hatchSpacing) {
      context.moveTo(x, area.bottom);
      context.lineTo(x + height, area.top);
    }

    context.stroke();
    context.restore();
  }

  for (const { x, items } of clusters) {
    if (x < area.left || x > area.right) {
      continue;
    }

    const several = items.length > 1;

    context.save();
    context.strokeStyle = colors.marker;
    context.fillStyle = colors.marker;
    context.lineWidth = several ? 2 : 1;
    context.beginPath();
    context.moveTo(x, area.top);
    context.lineTo(x, area.bottom);
    context.stroke();
    context.beginPath();
    context.arc(x, area.top + 4, several ? 4 : 2.5, 0, Math.PI * 2);
    context.fill();
    context.restore();
  }
}

/**
 * What the tooltip says about the marker and band under the pointer: the
 * newest changes first, the rest counted.
 */
export function tooltipMarkers(
  cluster: MarkerCluster | undefined,
  band: MarkerBand | undefined,
): TooltipMarkers | undefined {
  if (!cluster && !band) {
    return undefined;
  }

  const newest = [...(cluster?.items ?? [])].sort((one, other) => other.at - one.at);

  return {
    changes: newest.slice(0, tooltipChanges).map(({ at, entry }) => ({
      key: entry.id,
      time: formatDateTime(new Date(at * 1000), {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      }),
      text: describeChange(entry),
    })),
    more: Math.max(0, newest.length - tooltipChanges),
    gap: band ? describeGap(band.item.from, band.item.to) : undefined,
  };
}
