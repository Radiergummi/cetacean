import { CHART_COLORS, getChartColor } from "./chartColors";
import { describe, it, expect } from "vitest";

describe("chartColors", () => {
  it("exports 10 fallback colors", () => {
    expect(CHART_COLORS).toHaveLength(10);
  });

  it("getChartColor returns color by index with modulo wrapping", () => {
    expect(getChartColor(0)).toBe(CHART_COLORS[0]);
    expect(getChartColor(10)).toBe(CHART_COLORS[0]);
    expect(getChartColor(3)).toBe(CHART_COLORS[3]);
  });
});
