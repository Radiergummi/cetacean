import { buildDataset } from "./dataset";
import { demoHistory, filterHistory, writeAnnotation } from "./history";
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

describe("filterHistory on annotations", () => {
  const webshop = dataset.services.find(
    ({ Spec }) => Spec.Labels?.["com.docker.stack.namespace"] === "webshop",
  );
  const stackOf = (id: string) =>
    dataset.services.find(({ ID }) => ID === id)?.Spec.Labels?.["com.docker.stack.namespace"];

  writeAnnotation(
    {
      text: "Webshop freeze",
      tags: ["freeze"],
      timestamp: "2026-10-31T11:00:00Z",
      resources: [{ type: "stack", id: "webshop" }],
    },
    (_, id) => id,
  );

  const texts = (params: Record<string, string>) =>
    filterHistory(demoHistory(dataset, now), new URLSearchParams(params), stackOf)
      .filter(({ kind }) => kind === "annotation")
      .map(({ text }) => text);

  it("keeps a note on a service's stack for that service", () => {
    expect(texts({ type: "service", resourceId: webshop?.ID ?? "" })).toContain("Webshop freeze");
  });

  it("keeps a note on a service for that service's stack", () => {
    writeAnnotation(
      {
        text: "Checkout hotfix",
        tags: [],
        timestamp: "2026-10-31T12:00:00Z",
        resources: [{ type: "service", id: webshop?.ID ?? "" }],
      },
      (_, id) => id,
    );

    expect(texts({ type: "stack", resourceId: "webshop" })).toContain("Checkout hotfix");
    expect(texts({ type: "stack", resourceId: "monitoring" })).not.toContain("Checkout hotfix");
  });

  it("keeps the notes carrying a tag, the gaps, and no change", () => {
    const kept = filterHistory(
      demoHistory(dataset, now),
      new URLSearchParams({ tag: "freeze" }),
      stackOf,
    );

    expect(kept.filter(({ kind }) => kind === "annotation").map(({ text }) => text)).toEqual([
      "Webshop freeze",
    ]);
    expect(kept.some(({ kind }) => kind === "gap")).toBe(true);
    expect(kept.some(({ kind }) => kind === "change")).toBe(false);
  });
});

describe("demo annotation IDs", () => {
  it("stay put when a service goes away", () => {
    const ids = (services: typeof dataset.services) =>
      demoHistory({ ...dataset, services }, now)
        .filter(({ kind }) => kind === "annotation")
        .map(({ id }) => id);

    expect(ids(dataset.services.slice(0, -1))).toEqual(ids(dataset.services));
  });
});
