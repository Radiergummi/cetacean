import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import MetricsPanel from "./MetricsPanel";

// Mock TimeSeriesChart since it uses uPlot which needs a real DOM
vi.mock("./TimeSeriesChart", () => ({
  default: ({ title, range }: { title: string; range: string }) => (
    <div data-testid={`chart-${title}`}>
      {title} ({range})
    </div>
  ),
}));

describe("MetricsPanel", () => {
  const charts = [
    { title: "CPU", query: "cpu_query", unit: "%" },
    { title: "Memory", query: "mem_query", unit: "bytes" },
  ];

  it("renders all charts", () => {
    render(<MetricsPanel charts={charts} />);
    expect(screen.getByTestId("chart-CPU")).toBeInTheDocument();
    expect(screen.getByTestId("chart-Memory")).toBeInTheDocument();
  });

  it("renders range buttons", () => {
    render(<MetricsPanel charts={charts} />);
    expect(screen.getByText("1h")).toBeInTheDocument();
    expect(screen.getByText("6h")).toBeInTheDocument();
    expect(screen.getByText("24h")).toBeInTheDocument();
    expect(screen.getByText("7d")).toBeInTheDocument();
  });

  it("changes range on button click", () => {
    render(<MetricsPanel charts={charts} />);
    fireEvent.click(screen.getByText("6h"));
    expect(screen.getByText("CPU (6h)")).toBeInTheDocument();
  });

  it("renders refresh button", () => {
    render(<MetricsPanel charts={charts} />);
    expect(screen.getByText("Refresh")).toBeInTheDocument();
  });

  it("renders auto-refresh checkbox", () => {
    render(<MetricsPanel charts={charts} />);
    expect(screen.getByText("Auto (30s)")).toBeInTheDocument();
  });
});
