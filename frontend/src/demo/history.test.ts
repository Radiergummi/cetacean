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

    expect(result.map(({ kind }) => kind).sort()).toEqual(["change", "change", "gap"]);
    expect(
      result.every(({ kind, resourceId }) => kind === "gap" || resourceId === service?.ID),
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
