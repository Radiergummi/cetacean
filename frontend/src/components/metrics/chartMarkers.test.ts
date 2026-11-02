import {
  drawMarkers,
  fractionalIndex,
  layoutMarkers,
  timeAtIndex,
  type MarkerHit,
  markersAt,
  tooltipMarkers,
} from "./chartMarkers";
import type { HistoryEntry } from "@/api/types";
import { describeAnnotationTime, type TimelineItem } from "@/lib/timeline";
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

const note = (id: number, overrides: Partial<HistoryEntry> = {}): HistoryEntry => ({
  ...entry(id),
  kind: "annotation",
  type: "",
  action: "",
  resourceId: "",
  name: "",
  text: `note ${id}`,
  tags: [],
  ...overrides,
});

const flag = (id: number, at: number, overrides: Partial<HistoryEntry> = {}): TimelineItem => ({
  shape: "point",
  kind: "annotation",
  at,
  entry: note(id, overrides),
});

const region = (id: number, from: number, to: number): TimelineItem => ({
  shape: "range",
  kind: "annotation",
  from,
  to,
  entry: note(id),
});

const hit = (partial: Partial<MarkerHit>): MarkerHit => ({
  cluster: undefined,
  band: undefined,
  flag: undefined,
  regions: [],
  ...partial,
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

describe("layoutMarkers on annotations", () => {
  it("keeps annotation points out of change clusters", () => {
    const { clusters, flags } = layoutMarkers([point(1, 200), flag(2, 201)], timestamps, pixelFor);

    expect(clusters.map(({ items }) => items.map(({ entry: { id } }) => id))).toEqual([[1]]);
    expect(flags.map(({ items }) => items.map(({ entry: { id } }) => id))).toEqual([[2]]);
  });

  it("clusters annotation points among themselves", () => {
    const { flags } = layoutMarkers([flag(1, 200), flag(2, 202)], timestamps, pixelFor);

    expect(flags).toHaveLength(1);
    expect(flags[0]?.items).toHaveLength(2);
  });

  it("clips an annotation range to the samples", () => {
    const { bands, regions } = layoutMarkers([region(1, 0, 900)], timestamps, pixelFor);

    expect(bands).toEqual([]);
    expect(regions.map(({ left, right }) => [left, right])).toEqual([[0, 300]]);
  });
});

describe("markersAt", () => {
  it("finds the nearest marker within reach and the band under the pointer", () => {
    const layout = layoutMarkers([point(1, 200), gap(250, 350)], timestamps, pixelFor);

    expect(markersAt(layout, 104).cluster?.x).toBe(100);
    expect(markersAt(layout, 120).cluster).toBeUndefined();
    expect(markersAt(layout, 200).band).toBeDefined();
  });

  it("finds every range under the pointer", () => {
    const layout = layoutMarkers(
      [gap(150, 350), region(1, 150, 300), region(2, 220, 380)],
      timestamps,
      pixelFor,
    );
    const found = markersAt(layout, 160);

    expect(found.band?.item.kind).toBe("gap");
    expect(found.regions.map(({ item }) => item.entry.id)).toEqual([1, 2]);

    const markers = tooltipMarkers(found);

    expect(markers?.annotations.map(({ key }) => key)).toEqual([2, 1]);
    expect(markers?.gap).toBeDefined();
  });
});

describe("tooltipMarkers", () => {
  it("lists the newest changes first and counts the rest", () => {
    const items = [1, 2, 3, 4, 5, 6, 7].map((id) => point(id, 200 + id / 10));
    const { clusters } = layoutMarkers(items, timestamps, pixelFor);
    const markers = tooltipMarkers(hit({ cluster: clusters[0] }));

    expect(markers?.changes.map(({ key }) => key)).toEqual([7, 6, 5, 4, 3]);
    expect(markers?.more).toBe(2);
  });

  it("says nothing away from markers and bands", () => {
    expect(tooltipMarkers(hit({}))).toBeUndefined();
  });

  it("describes an annotation", () => {
    const { flags } = layoutMarkers(
      [
        flag(1, 200, {
          text: "Deployed the fix",
          tags: ["deploy"],
          actor: { subject: "u1", name: "alice", provider: "oidc", transport: "dashboard" },
          updated: "2026-11-01T15:00:00Z",
        }),
        flag(2, 201),
      ],
      timestamps,
      pixelFor,
    );
    const markers = tooltipMarkers(hit({ flag: flags[0] }));

    expect(markers?.annotations).toEqual([
      { key: 2, text: "note 2", detail: describeAnnotationTime(201) },
      {
        key: 1,
        text: "Deployed the fix",
        detail: `${describeAnnotationTime(200)} · #deploy · by alice via the dashboard · edited`,
      },
    ]);
  });

  it("lists at most five annotations and counts the rest", () => {
    const { flags } = layoutMarkers(
      [1, 2, 3, 4, 5, 6, 7].map((id) => flag(id, 200 + id / 10)),
      timestamps,
      pixelFor,
    );
    const markers = tooltipMarkers(hit({ flag: flags[0] }));

    expect(markers?.annotations.map(({ key }) => key)).toEqual([7, 6, 5, 4, 3]);
    expect(markers?.moreAnnotations).toBe(2);
  });
});

describe("drawMarkers", () => {
  it("draws a range under the changes and a flag over them", () => {
    const calls: string[] = [];
    const nothing = () => {};
    const style = { fillStyle: "", strokeStyle: "", lineWidth: 1, globalAlpha: 1 };
    const context = Object.assign(style, {
      save: nothing,
      beginPath: nothing,
      closePath: nothing,
      rect: nothing,
      clip: nothing,
      moveTo: nothing,
      lineTo: nothing,
      arc: nothing,
      restore: () => {
        style.globalAlpha = 1;
      },
      fill: () => calls.push(`fill ${style.fillStyle} ${style.globalAlpha}`),
      stroke: () => calls.push(`stroke ${style.strokeStyle} ${style.globalAlpha}`),
    }) as unknown as CanvasRenderingContext2D;
    const layout = layoutMarkers(
      [gap(100, 200), region(1, 200, 300), point(2, 250), flag(3, 260)],
      timestamps,
      pixelFor,
    );

    drawMarkers(context, { top: 0, bottom: 100, left: 0, right: 300 }, layout, {
      marker: "change",
      cluster: "several",
      gap: "gap",
      annotation: "note",
    });

    expect(calls).toEqual([
      "fill gap 1",
      "stroke gap 1",
      "fill note 0.15",
      "stroke change 1",
      "fill change 1",
      "stroke note 1",
      "fill note 1",
    ]);
  });
});

describe("timeAtIndex", () => {
  it("finds the time at a fractional index, clamped to the samples", () => {
    expect(timeAtIndex([100, 200, 300], 1.5)).toBe(250);
    expect(timeAtIndex([100, 200, 300], -1)).toBe(100);
    expect(timeAtIndex([100, 200, 300], 9)).toBe(300);
    expect(timeAtIndex([], 0)).toBeNull();
  });
});
