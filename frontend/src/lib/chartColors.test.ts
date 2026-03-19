import { chartColors, getChartColor } from "./chartColors";
import { describe, it, expect } from "vitest";

describe("chartColors", () => {
  it("exports 10 fallback colors", () => {
    expect(chartColors).toHaveLength(10);
  });

  it("getChartColor returns color by index with modulo wrapping", () => {
    expect(getChartColor(0)).toBe(chartColors[0]);
    expect(getChartColor(10)).toBe(chartColors[0]);
    expect(getChartColor(3)).toBe(chartColors[3]);
  });
});
