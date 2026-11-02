import type { TooltipMarkers } from "./ChartTooltipOverlay";
import { attribution } from "@/lib/activity";
import { formatDateTime } from "@/lib/format";
import {
  dayAndTime,
  describeAnnotationTime,
  describeChange,
  describeGap,
  type TimelineItem,
} from "@/lib/timeline";

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
 * The radius of the cap on a marker's line. The colours are opaque: a cap
 * over a translucent line shows where the two overlap.
 */
const capRadius = 3.5;

/**
 * The spacing of a gap's hatching, in pixels.
 */
const hatchSpacing = 6;

/**
 * The opacity of a range annotation's band.
 */
const rangeAlpha = 0.15;

/**
 * The height of the pennant on an annotation's line, and twice its reach.
 */
const flagSize = 7;

export interface MarkerCluster {
  x: number;
  items: PointItem[];
}

export interface MarkerBand {
  left: number;
  right: number;
  item: RangeItem;
}

/**
 * Changes cluster and gaps band; annotations get their own of each, so a
 * note is never folded into a change or drawn as a gap.
 */
export interface MarkerLayout {
  clusters: MarkerCluster[];
  bands: MarkerBand[];
  flags: MarkerCluster[];
  regions: MarkerBand[];
}

/**
 * What lies under the pointer: every annotation range, since they overlap.
 */
export interface MarkerHit {
  cluster: MarkerCluster | undefined;
  band: MarkerBand | undefined;
  flag: MarkerCluster | undefined;
  regions: MarkerBand[];
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
 * The time at a fractional sample index, the inverse of `fractionalIndex`,
 * clamped to the first and last sample.
 */
export function timeAtIndex(timestamps: number[], index: number): number | null {
  const last = timestamps.length - 1;

  if (last < 0) {
    return null;
  }

  const clamped = Math.min(Math.max(index, 0), last);
  const low = Math.floor(clamped);
  const start = timestamps[low] ?? 0;
  const end = timestamps[Math.min(low + 1, last)] ?? start;

  return start + (end - start) * (clamped - low);
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
  const changes: { x: number; item: PointItem }[] = [];
  const notes: { x: number; item: PointItem }[] = [];
  const layout: MarkerLayout = { clusters: [], bands: [], flags: [], regions: [] };

  if (first === undefined || last === undefined) {
    return layout;
  }

  for (const item of items) {
    if (item.shape === "point") {
      const index = fractionalIndex(timestamps, item.at);

      if (index !== null) {
        (item.kind === "annotation" ? notes : changes).push({ x: pixelFor(index), item });
      }

      continue;
    }

    if (item.to <= first || item.from >= last) {
      continue;
    }

    const left = fractionalIndex(timestamps, Math.max(item.from, first)) ?? 0;
    const right = fractionalIndex(timestamps, Math.min(item.to, last)) ?? timestamps.length - 1;

    (item.kind === "annotation" ? layout.regions : layout.bands).push({
      left: pixelFor(left),
      right: pixelFor(right),
      item,
    });
  }

  layout.clusters = cluster(changes);
  layout.flags = cluster(notes);

  return layout;
}

function cluster(points: { x: number; item: PointItem }[]): MarkerCluster[] {
  points.sort((one, other) => one.x - other.x);

  const clusters: MarkerCluster[] = [];
  let start = Number.NEGATIVE_INFINITY;

  for (const { x, item } of points) {
    const last = clusters.at(-1);

    if (last && x - start < markerRadius) {
      last.items.push(item);
      last.x = start + (x - start) / 2;

      continue;
    }

    start = x;
    clusters.push({ x, items: [item] });
  }

  return clusters;
}

function nearest(clusters: MarkerCluster[], x: number): MarkerCluster | undefined {
  let found: MarkerCluster | undefined;

  for (const candidate of clusters) {
    const distance = Math.abs(candidate.x - x);

    if (distance <= markerRadius && (!found || distance < Math.abs(found.x - x))) {
      found = candidate;
    }
  }

  return found;
}

/**
 * The markers and bands under the pointer, if any.
 */
export function markersAt(layout: MarkerLayout | null, x: number): MarkerHit {
  const within = ({ left, right }: MarkerBand) => x >= left && x <= right;

  return {
    cluster: nearest(layout?.clusters ?? [], x),
    band: layout?.bands.find(within),
    flag: nearest(layout?.flags ?? [], x),
    regions: layout?.regions.filter(within) ?? [],
  };
}

/**
 * Draws gaps as hatched shading, annotation ranges as plain shading over them,
 * changes as lines with a round cap, and annotation points as lines with a
 * pennant on top. A cluster of several gets a wider line.
 */
export function drawMarkers(
  context: CanvasRenderingContext2D,
  area: { top: number; bottom: number; left: number; right: number },
  { clusters, bands, flags, regions }: MarkerLayout,
  colors: { marker: string; cluster: string; gap: string; annotation: string },
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

  for (const { left, right } of regions) {
    const clippedLeft = Math.max(left, area.left);
    const clippedRight = Math.min(right, area.right);

    context.save();
    context.globalAlpha = rangeAlpha;
    context.fillStyle = colors.annotation;
    context.beginPath();
    context.rect(clippedLeft, area.top, clippedRight - clippedLeft, height);
    context.fill();
    context.restore();
  }

  for (const { x, items } of clusters) {
    if (x < area.left || x > area.right) {
      continue;
    }

    const several = items.length > 1;
    const capTop = area.top + capRadius;

    context.save();
    context.strokeStyle = several ? colors.cluster : colors.marker;
    context.fillStyle = context.strokeStyle;
    context.lineWidth = several ? 2 : 1;
    context.beginPath();
    context.moveTo(x, capTop);
    context.lineTo(x, area.bottom);
    context.stroke();
    context.beginPath();
    context.arc(x, capTop, capRadius, 0, Math.PI * 2);
    context.fill();
    context.restore();
  }

  for (const { x, items } of flags) {
    if (x < area.left || x > area.right) {
      continue;
    }

    context.save();
    context.strokeStyle = colors.annotation;
    context.fillStyle = colors.annotation;
    context.lineWidth = items.length > 1 ? 2 : 1.5;
    context.beginPath();
    context.moveTo(x, area.top);
    context.lineTo(x, area.bottom);
    context.stroke();
    context.beginPath();
    context.moveTo(x, area.top);
    context.lineTo(x + flagSize, area.top + flagSize / 2);
    context.lineTo(x, area.top + flagSize);
    context.closePath();
    context.fill();
    context.restore();
  }
}

/**
 * What the tooltip says about what is under the pointer: annotations and
 * changes newest first, each list capped and the rest counted.
 */
export function tooltipMarkers({
  cluster,
  band,
  flag,
  regions,
}: MarkerHit): TooltipMarkers | undefined {
  if (!cluster && !band && !flag && regions.length === 0) {
    return undefined;
  }

  const newest = [...(cluster?.items ?? [])].sort((one, other) => other.at - one.at);
  const notes = [
    ...(flag?.items ?? []).map(({ at, entry }) => ({ start: at, end: undefined, entry })),
    ...regions.map(({ item }) => ({
      start: item.from,
      end: item.to,
      entry: item.entry,
    })),
  ].sort((one, other) => other.start - one.start);

  return {
    changes: newest.slice(0, tooltipChanges).map(({ at, entry }) => ({
      key: entry.id,
      time: formatDateTime(new Date(at * 1000), dayAndTime),
      text: describeChange(entry),
    })),
    more: Math.max(0, newest.length - tooltipChanges),
    annotations: notes.slice(0, tooltipChanges).map(({ start, end, entry }) => ({
      key: entry.id,
      text: entry.text ?? "",
      detail: [
        describeAnnotationTime(start, end),
        ...(entry.tags ?? []).map((tag) => `#${tag}`),
        entry.actor ? attribution(entry.actor) : undefined,
        entry.updated === undefined ? undefined : "edited",
      ]
        .filter(Boolean)
        .join(" · "),
    })),
    moreAnnotations: Math.max(0, notes.length - tooltipChanges),
    gap: band ? describeGap(band.item.from, band.item.to) : undefined,
  };
}
