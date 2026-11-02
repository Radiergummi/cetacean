import { buildDataset } from "./dataset";
import { demoHistory, filterHistory } from "./history";
import { describe, expect, it } from "vitest";

const now = Date.parse("2026-10-31T12:00:00Z");
const dataset = buildDataset();
const entries = demoHistory(dataset, now);

describe("filterHistory", () => {
  it("keeps one resource's changes and every gap overlapping the window", () => {
    const service = dataset.services[0];
    const result = filterHistory(
      entries,
      new URLSearchParams({
        type: "service",
        resourceId: service?.ID ?? "",
        since: "2026-10-31T02:00:00Z",
      }),
    );

    expect(result.map(({ kind }) => kind).sort()).toEqual([
      "annotation",
      "annotation",
      "change",
      "change",
      "gap",
    ]);
    expect(
      result.every(({ kind, resourceId }) => kind !== "change" || resourceId === service?.ID),
    ).toBe(true);
  });

  it("drops a gap that ended before the window", () => {
    const result = filterHistory(
      entries,
      new URLSearchParams({ kind: "gap", since: "2026-10-31T05:00:00Z" }),
    );

    expect(result).toEqual([]);
  });
});

describe("demo annotations", () => {
  const annotations = entries.filter(({ kind }) => kind === "annotation");
  const ids = (params: Record<string, string>) =>
    filterHistory(entries, new URLSearchParams({ kind: "annotation", ...params })).map(
      ({ text }) => text,
    );

  it("seeds one point and one range annotation", () => {
    expect(annotations.map(({ until }) => until === undefined).sort()).toEqual([false, true]);
    expect(annotations.every(({ text, actor }) => text && actor)).toBe(true);
    expect(annotations.some(({ updated, editor }) => updated && editor)).toBe(true);
  });

  it("filters annotations as /history does", () => {
    const [first, second] = dataset.services;
    const point = annotations.find(({ until }) => until === undefined)?.text;
    const range = annotations.find(({ until }) => until !== undefined)?.text;

    expect(ids({}).sort()).toEqual([point, range].sort());
    expect(ids({ type: "service", resourceId: first?.ID ?? "" }).sort()).toEqual(
      [point, range].sort(),
    );
    expect(ids({ type: "service", resourceId: second?.ID ?? "" })).toEqual([range]);
    expect(ids({ type: "node" })).toEqual([range]);
    expect(ids({ since: "2026-10-31T09:10:00Z", until: "2026-10-31T09:20:00Z" })).toEqual([range]);
  });
});
