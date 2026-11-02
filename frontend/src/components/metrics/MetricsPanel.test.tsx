import MetricsPanel from "./MetricsPanel";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi } from "vitest";

// Mock TimeSeriesChart since it uses Chart.js, which needs a real DOM
vi.mock("./TimeSeriesChart", () => ({
  default: ({ title, range }: { title: string; range: string }) => (
    <div data-testid={`chart-${title}`}>
      {title} ({range})
    </div>
  ),
}));

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe("MetricsPanel", () => {
  const charts = [
    { title: "CPU", query: "cpu_query", unit: "%" },
    { title: "Memory", query: "mem_query", unit: "bytes" },
  ];

  it("renders all charts", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByTestId("chart-CPU")).toBeInTheDocument();
    expect(screen.getByTestId("chart-Memory")).toBeInTheDocument();
  });

  it("renders range buttons", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByText("1H")).toBeInTheDocument();
    expect(screen.getByText("6H")).toBeInTheDocument();
    expect(screen.getByText("24H")).toBeInTheDocument();
    expect(screen.getByText("7D")).toBeInTheDocument();
  });

  it("changes range on button click", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    fireEvent.click(screen.getByText("6H"));
    expect(screen.getByText("CPU (6h)")).toBeInTheDocument();
  });

  it("renders refresh button", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByTitle("Refresh")).toBeInTheDocument();
  });

  it("renders streaming toggle", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByTitle("Pause live streaming")).toBeInTheDocument();
  });

  it("names the custom range trigger", () => {
    render(<MetricsPanel charts={charts} />, { wrapper });
    expect(screen.getByRole("button", { name: "Custom range" })).toBeInTheDocument();
  });

  it("offers Annotate only with a scope", () => {
    const { unmount } = render(<MetricsPanel charts={charts} />, { wrapper });

    expect(screen.queryByRole("button", { name: "Annotate" })).not.toBeInTheDocument();
    unmount();

    render(
      <MetricsPanel
        charts={charts}
        annotate={{ type: "service", id: "svc1", name: "web" }}
      />,
      { wrapper },
    );

    expect(screen.getByRole("button", { name: "Annotate" })).toBeInTheDocument();
  });

  it("leaves annotate mode on Escape", () => {
    render(
      <MetricsPanel
        charts={charts}
        annotate={{ type: "service", id: "svc1", name: "web" }}
      />,
      { wrapper },
    );

    const toggle = screen.getByRole("button", { name: "Annotate" });

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");

    fireEvent.keyDown(document, { key: "Escape" });
    expect(toggle).not.toHaveAttribute("aria-pressed");
  });
});
