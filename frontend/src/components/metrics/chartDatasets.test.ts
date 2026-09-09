import { buildTooltipSeries, computeSuggestedMax } from "./chartDatasets";
import type { ParsedMetrics } from "@/lib/metricsParser.ts";
import type { ChartDataset } from "chart.js";
import { describe, expect, it } from "vitest";

/**
 * The tooltip rows and the axis ceiling were only reachable through a canvas
 * mousemove, which jsdom cannot dispatch — so neither had ever been tested.
 * Pulling them out of the component is what makes that possible.
 */

function dataset(label: string, color: string, data: number[]): ChartDataset<"line"> {
  return { label, borderColor: color, data } as ChartDataset<"line">;
}

function metrics(series: { label: string; data: number[] }[]): ParsedMetrics {
  return {
    labels: [],
    timestamps: [],
    series: series.map(({ data, label }) => ({ label, color: "#000", data })),
  };
}

describe("buildTooltipSeries", () => {
  const datasets = [dataset("alpha", "#111", [1, 9]), dataset("beta", "#222", [5, 2])];

  it("lists the series at one point, largest first", () => {
    const rows = buildTooltipSeries({
      datasets,
      index: 0,
      unit: undefined,
      thresholds: undefined,
      stackedSeries: null,
    });

    expect(rows.map(({ label }) => label)).toEqual(["beta", "alpha"]);
    expect(rows[0]).toMatchObject({ color: "#222", raw: 5 });
  });

  it("reorders as the values cross", () => {
    const rows = buildTooltipSeries({
      datasets,
      index: 1,
      unit: undefined,
      thresholds: undefined,
      stackedSeries: null,
    });

    expect(rows.map(({ label }) => label)).toEqual(["alpha", "beta"]);
  });

  it("ranks a threshold among the values it is there to be read against", () => {
    const rows = buildTooltipSeries({
      datasets,
      index: 0,
      unit: undefined,
      thresholds: [{ label: "limit", value: 3, color: "#f00" }],
      stackedSeries: null,
    });

    expect(rows.map(({ label }) => label)).toEqual(["beta", "limit", "alpha"]);
    expect(rows[1]?.dashed).toBe(true);
  });

  it("leads a stack with its total, which no single row states", () => {
    const rows = buildTooltipSeries({
      datasets,
      index: 0,
      unit: undefined,
      thresholds: undefined,
      stackedSeries: metrics([
        { label: "alpha", data: [1, 9] },
        { label: "beta", data: [5, 2] },
      ]).series,
    });

    expect(rows[0]).toMatchObject({ label: "Total", raw: 6 });
    expect(rows.slice(1).map(({ label }) => label)).toEqual(["beta", "alpha"]);
  });

  it("skips a series with no value at that point rather than reading it as zero", () => {
    const rows = buildTooltipSeries({
      datasets: [dataset("alpha", "#111", [1]), dataset("gap", "#222", [])],
      index: 0,
      unit: undefined,
      thresholds: undefined,
      stackedSeries: null,
    });

    expect(rows.map(({ label }) => label)).toEqual(["alpha"]);
  });

  it("formats through the unit it was given", () => {
    const rows = buildTooltipSeries({
      datasets: [dataset("alpha", "#111", [0.5])],
      index: 0,
      unit: "%",
      thresholds: undefined,
      stackedSeries: null,
    });

    expect(rows[0]?.value).toContain("%");
  });
});

describe("computeSuggestedMax", () => {
  const data = metrics([{ label: "alpha", data: [10, 20] }]);

  it("leaves the axis alone when there is no threshold to keep in view", () => {
    expect(computeSuggestedMax(data, undefined, undefined)).toBeUndefined();
    expect(computeSuggestedMax(data, [], undefined)).toBeUndefined();
    expect(computeSuggestedMax(null, [{ label: "l", value: 1, color: "#f00" }], undefined)).toBe(
      undefined,
    );
  });

  it("lifts the ceiling over a threshold above the data", () => {
    expect(computeSuggestedMax(data, [{ label: "limit", value: 100, color: "#f00" }], 0)).toBe(110);
  });

  it("keeps headroom over the data when the threshold sits below it", () => {
    expect(computeSuggestedMax(data, [{ label: "limit", value: 5, color: "#f00" }], 0)).toBe(22);
  });

  it("measures headroom from the data floor when none is forced", () => {
    // High 20, low 10 — a tenth of the span rather than of the value.
    expect(
      computeSuggestedMax(data, [{ label: "limit", value: 5, color: "#f00" }], undefined),
    ).toBe(21);
  });

  it("gives a flat chart no headroom, so its threshold sits on the top edge", () => {
    const flat = metrics([{ label: "alpha", data: [7, 7] }]);

    expect(computeSuggestedMax(flat, [{ label: "limit", value: 7, color: "#f00" }], 7)).toBe(7);
  });

  it("falls back to a ceiling of one when there is nothing to scale to", () => {
    // The `|| high + 1` guard only fires on an all-zero chart, which is the one
    // case where the computed ceiling would be zero and the axis degenerate.
    const zero = metrics([{ label: "alpha", data: [0, 0] }]);

    expect(computeSuggestedMax(zero, [{ label: "limit", value: 0, color: "#f00" }], 0)).toBe(1);
  });
});
