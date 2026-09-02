import { MetricsView } from "./MetricsView";
import type { MetricsResult } from "./types";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// Chart.js needs a canvas jsdom does not provide; the chart is mocked here and
// exercised by rendering the widget for real. What this file asserts is the
// frame around it — range, legend, readout, empty state.
vi.mock("./MetricsChart", () => ({
  MetricsChart: ({ series }: { series: { name: string }[] }) => (
    <div data-testid="chart">{series.map(({ name }) => name).join(",")}</div>
  ),
}));

function result(overrides: Partial<MetricsResult> = {}): MetricsResult {
  return {
    target: "service",
    id: "svc1",
    name: "monitoring_prometheus",
    metric: "cpu",
    unit: "percent",
    range: "1h",
    series: [
      {
        name: "cpu",
        points: [
          { time: "2026-09-01T09:00:00Z", value: 12.5 },
          { time: "2026-09-01T09:01:00Z", value: 18.25 },
        ],
      },
    ],
    ...overrides,
  };
}

describe("MetricsView", () => {
  it("charts the series it is given", () => {
    render(
      <MetricsView
        result={result()}
        onRangeChange={vi.fn<(range: string) => void>()}
      />,
    );

    expect(screen.getByTestId("chart")).toHaveTextContent("cpu");
    expect(screen.getByText(/monitoring_prometheus/)).toBeInTheDocument();
  });

  it("asks for a new range when one is picked", () => {
    const onRangeChange = vi.fn<(range: string) => void>();

    render(
      <MetricsView
        result={result()}
        onRangeChange={onRangeChange}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "24h" }));

    expect(onRangeChange).toHaveBeenCalledWith("24h");
  });

  it("says a metric has no data rather than showing an empty chart", () => {
    render(
      <MetricsView
        result={result({ series: [{ name: "cpu", points: [] }] })}
        onRangeChange={vi.fn<(range: string) => void>()}
      />,
    );

    expect(screen.queryByTestId("chart")).not.toBeInTheDocument();
    expect(screen.getByText(/no data/i)).toBeInTheDocument();
  });

  // Two series are told apart by a legend and by their latest values in text,
  // never by color alone — the palette's blue is below 3:1 against a light
  // surface, which obliges visible labels.
  it("names every series in a legend, with its latest value", () => {
    render(
      <MetricsView
        result={result({
          metric: "network",
          unit: "bytes/s",
          series: [
            { name: "receive", points: [{ time: "2026-09-01T09:00:00Z", value: 2048 }] },
            { name: "transmit", points: [{ time: "2026-09-01T09:00:00Z", value: 1024 }] },
          ],
        })}
        onRangeChange={vi.fn<(range: string) => void>()}
      />,
    );

    const legend = screen.getByRole("list", { name: /series/i });

    expect(legend).toHaveTextContent("receive");
    expect(legend).toHaveTextContent("transmit");
    expect(legend).toHaveTextContent("2kB/s");
    expect(legend).toHaveTextContent("1kB/s");
  });

  it("reads a percentage as a percentage", () => {
    render(
      <MetricsView
        result={result()}
        onRangeChange={vi.fn<(range: string) => void>()}
      />,
    );

    expect(screen.getByText("18.3%")).toBeInTheDocument();
  });
});
