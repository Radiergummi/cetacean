import {
  appendMetricPoint,
  formatMetricIdentifier,
  normalizePrometheusRows,
  parseRangeResult,
  seriesChanged,
  seriesLabel,
  type ParsedMetrics,
} from "./metricsParser";
import type { PrometheusResponse } from "@/api/types.ts";
import { describe, it, expect, vi } from "vitest";

vi.mock("@/lib/chartColors.ts", () => ({
  getChartColor: (index: number) => `color-${index}`,
}));

describe("seriesLabel", () => {
  it("returns fallback for undefined metric", () => {
    expect(seriesLabel(undefined)).toBe("value");
    expect(seriesLabel(undefined, "custom")).toBe("custom");
  });

  it("returns label values joined by comma", () => {
    expect(seriesLabel({ instance: "node1", job: "cadvisor" })).toBe("node1, cadvisor");
  });

  it("prefers label values over __name__", () => {
    expect(seriesLabel({ __name__: "cpu_usage", instance: "node1" })).toBe("node1");
  });

  it("falls back to __name__ when no other labels", () => {
    expect(seriesLabel({ __name__: "up" })).toBe("up");
  });

  it("returns fallback when metric is empty", () => {
    expect(seriesLabel({})).toBe("value");
    expect(seriesLabel({}, "total")).toBe("total");
  });

  it("filters out empty label values", () => {
    expect(seriesLabel({ instance: "node1", job: "" })).toBe("node1");
  });
});

describe("parseRangeResult", () => {
  it("returns null for empty result", () => {
    const response = { data: { resultType: "matrix" as const, result: [] } };
    expect(parseRangeResult(response, "CPU")).toBeNull();
  });

  it("returns null for missing data", () => {
    expect(parseRangeResult({} as PrometheusResponse, "CPU")).toBeNull();
  });

  it("parses a single series", () => {
    const response: PrometheusResponse = {
      data: {
        resultType: "matrix",
        result: [
          {
            metric: { __name__: "cpu_usage" },
            values: [
              [1000, "0.5"],
              [1015, "0.7"],
            ],
          },
        ],
      },
    };

    const parsed = parseRangeResult(response, "CPU");

    expect(parsed).not.toBeNull();
    expect(parsed!.timestamps).toEqual([1000, 1015]);
    expect(parsed!.labels).toHaveLength(2);
    expect(parsed!.series).toHaveLength(1);
    expect(parsed!.series[0]?.label).toBe("cpu_usage");
    expect(parsed!.series[0]?.data).toEqual([0.5, 0.7]);
    expect(parsed!.series[0]?.color).toBe("color-0");
  });

  it("uses metric labels for multi-series", () => {
    const response: PrometheusResponse = {
      data: {
        resultType: "matrix",
        result: [
          {
            metric: { instance: "node1" },
            values: [[1000, "1"]],
          },
          {
            metric: { instance: "node2" },
            values: [[1000, "2"]],
          },
        ],
      },
    };

    const parsed = parseRangeResult(response, "CPU");

    expect(parsed!.series[0]?.label).toBe("node1");
    expect(parsed!.series[1]?.label).toBe("node2");
    expect(parsed!.series[0]?.color).toBe("color-0");
    expect(parsed!.series[1]?.color).toBe("color-1");
  });

  it("applies color override to all series", () => {
    const response: PrometheusResponse = {
      data: {
        resultType: "matrix",
        result: [
          { metric: { instance: "a" }, values: [[1000, "1"]] },
          { metric: { instance: "b" }, values: [[1000, "2"]] },
        ],
      },
    };

    const parsed = parseRangeResult(response, "CPU", "#ff0000");

    expect(parsed!.series[0]?.color).toBe("#ff0000");
    expect(parsed!.series[1]?.color).toBe("#ff0000");
  });
});

describe("seriesChanged", () => {
  const makeParsed = (labels: string[]): ParsedMetrics => ({
    labels: [],
    timestamps: [],
    series: labels.map((label) => ({ label, color: "", data: [] })),
  });

  it("returns true when previous is null", () => {
    expect(seriesChanged(null, makeParsed(["a"]))).toBe(true);
  });

  it("returns true when series count differs", () => {
    expect(seriesChanged(makeParsed(["a"]), makeParsed(["a", "b"]))).toBe(true);
  });

  it("returns true when labels differ", () => {
    expect(seriesChanged(makeParsed(["a", "b"]), makeParsed(["a", "c"]))).toBe(true);
  });

  it("returns false when labels match", () => {
    expect(seriesChanged(makeParsed(["a", "b"]), makeParsed(["a", "b"]))).toBe(false);
  });
});

describe("formatMetricIdentifier", () => {
  it("formats name with labels", () => {
    expect(
      formatMetricIdentifier({ __name__: "cpu_usage", instance: "node1", job: "cadvisor" }),
    ).toBe('cpu_usage{instance="node1", job="cadvisor"}');
  });

  it("formats name without labels", () => {
    expect(formatMetricIdentifier({ __name__: "up" })).toBe("up");
  });

  it("formats labels without name", () => {
    expect(formatMetricIdentifier({ instance: "node1" })).toBe('{instance="node1"}');
  });

  it("returns empty braces for empty metric", () => {
    expect(formatMetricIdentifier({})).toBe("{}");
  });
});

describe("normalizePrometheusRows", () => {
  it("normalizes vector results", () => {
    const data = {
      resultType: "vector" as const,
      result: [
        { metric: { __name__: "up" }, value: [1000, "1"] as [number, string] },
        { metric: { __name__: "down" }, value: [1000, "0"] as [number, string] },
      ],
    };
    const rows = normalizePrometheusRows(data);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toEqual({ metric: { __name__: "up" }, value: "1", timestamp: 1000 });
    expect(rows[1]).toEqual({ metric: { __name__: "down" }, value: "0", timestamp: 1000 });
  });

  it("uses last value for matrix results", () => {
    const data = {
      resultType: "matrix" as const,
      result: [
        {
          metric: { __name__: "cpu" },
          values: [
            [1000, "0.5"],
            [1015, "0.7"],
          ] as [number, string][],
        },
      ],
    };
    const rows = normalizePrometheusRows(data);
    expect(rows).toHaveLength(1);
    expect(rows[0]?.value).toBe("0.7");
    expect(rows[0]?.timestamp).toBe(1015);
  });

  it("filters out results with no data points", () => {
    const data = {
      resultType: "vector" as const,
      result: [{ metric: { __name__: "empty" } }],
    };
    const rows = normalizePrometheusRows(data);
    expect(rows).toHaveLength(0);
  });
});

describe("appendMetricPoint", () => {
  const previous = {
    labels: ["10:00:00", "10:00:15"],
    timestamps: [1000, 1015],
    series: [
      { label: "web", color: "#111", data: [1, 2] },
      { label: "api", color: "#222", data: [3, 4] },
    ],
  };

  function pointResponse(samples: [string, string][]) {
    return {
      data: {
        resultType: "vector",
        result: samples.map(([job, value]) => ({
          metric: { job },
          value: [1030, value] as [number, string],
        })),
      },
    } as unknown as PrometheusResponse;
  }

  it("drops the oldest sample and appends the newest", () => {
    const next = appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(next?.timestamps).toEqual([1015, 1030]);
    expect(next?.series[0]?.data).toEqual([2, 9]);
  });

  it("keeps each series matched to its own label", () => {
    const next = appendMetricPoint(
      previous,
      pointResponse([
        ["api", "8"],
        ["web", "7"],
      ]),
    );

    expect(next?.series[0]?.data).toEqual([2, 7]);
    expect(next?.series[1]?.data).toEqual([4, 8]);
  });

  it("appends a zero for a series the response omits", () => {
    const next = appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(next?.series[1]?.data).toEqual([4, 0]);
  });

  it("preserves series colour and label", () => {
    const next = appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(next?.series[0]).toMatchObject({ label: "web", color: "#111" });
  });

  it("returns null when the response carries no samples", () => {
    expect(appendMetricPoint(previous, pointResponse([]))).toBeNull();
  });

  it("does not mutate the previous window", () => {
    appendMetricPoint(previous, pointResponse([["web", "9"]]));

    expect(previous.timestamps).toEqual([1000, 1015]);
    expect(previous.series[0]?.data).toEqual([1, 2]);
  });
});
