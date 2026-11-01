import { fractionalIndex, layoutMarkers, markersAt, tooltipMarkers } from "./chartMarkers";
import type { HistoryEntry } from "@/api/types";
import type { TimelineItem } from "@/lib/timeline";
import { describe, expect, it } from "vitest";

const timestamps = [100, 200, 300, 400];
const pixelFor = (index: number) => index * 100;

const entry = (id: number): HistoryEntry => ({
  id,
  kind: "change",
  timestamp: "",
  type: "service",
  action: "update",
  resourceId: "svc1",
  name: "web",
});

const point = (id: number, at: number): TimelineItem => ({
  shape: "point",
  kind: "change",
  at,
  entry: entry(id),
});

const gap = (from: number, to: number): TimelineItem => ({
  shape: "range",
  kind: "gap",
  from,
  to,
  entry: { ...entry(0), kind: "gap" },
});

describe("fractionalIndex", () => {
  it("interpolates between the samples around a time", () => {
    expect(fractionalIndex(timestamps, 250)).toBe(1.5);
    expect(fractionalIndex(timestamps, 100)).toBe(0);
  });

  it("pins a time within a step past the last sample to it", () => {
    expect(fractionalIndex(timestamps, 450)).toBe(3);
    expect(fractionalIndex(timestamps, 501)).toBeNull();
  });

  it("pins a time within a step before the first sample to it", () => {
    expect(fractionalIndex(timestamps, 50)).toBe(0);
    expect(fractionalIndex(timestamps, -1)).toBeNull();
  });

  it("has no place for a time without samples", () => {
    expect(fractionalIndex([], 100)).toBeNull();
  });
});

describe("layoutMarkers", () => {
  it("merges points closer than the marker radius", () => {
    const { clusters } = layoutMarkers(
      [point(1, 200), point(2, 202), point(3, 300)],
      timestamps,
      pixelFor,
    );

    expect(clusters.map(({ items }) => items.map(({ entry: { id } }) => id))).toEqual([
      [1, 2],
      [3],
    ]);
  });

  it("clips a gap that began before the window to its start", () => {
    const { bands } = layoutMarkers([gap(0, 250), gap(500, 600)], timestamps, pixelFor);

    expect(bands.map(({ left, right }) => [left, right])).toEqual([[0, 150]]);
  });
});

describe("markersAt", () => {
  it("finds the nearest marker within reach and the band under the pointer", () => {
    const layout = layoutMarkers([point(1, 200), gap(250, 350)], timestamps, pixelFor);

    expect(markersAt(layout, 104).cluster?.x).toBe(100);
    expect(markersAt(layout, 120).cluster).toBeUndefined();
    expect(markersAt(layout, 200).band).toBeDefined();
  });
});

describe("tooltipMarkers", () => {
  it("lists the newest changes first and counts the rest", () => {
    const items = [1, 2, 3, 4, 5, 6, 7].map((id) => point(id, 200 + id / 10));
    const { clusters } = layoutMarkers(items, timestamps, pixelFor);
    const markers = tooltipMarkers(clusters[0], undefined);

    expect(markers?.changes.map(({ key }) => key)).toEqual([7, 6, 5, 4, 3]);
    expect(markers?.more).toBe(2);
  });

  it("says nothing away from markers and bands", () => {
    expect(tooltipMarkers(undefined, undefined)).toBeUndefined();
  });
});
