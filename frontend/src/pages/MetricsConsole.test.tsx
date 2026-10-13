import { createTestQueryClient } from "../test/mocks";
import MetricsConsole from "./MetricsConsole";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

vi.mock("../api/client", () => ({
  api: {
    monitoringStatus: vi.fn<() => Promise<unknown>>().mockResolvedValue({
      prometheusConfigured: true,
      prometheusReachable: true,
      queryable: false,
      nodeExporter: { targets: 3, nodes: 3 },
      cadvisor: { targets: 3, nodes: 3 },
    }),
    metricsQuery: vi.fn<() => Promise<unknown>>(),
    metricsLabels: vi.fn<() => Promise<unknown>>(),
    metricsLabelValues: vi.fn<() => Promise<unknown>>(),
  },
}));

import { api } from "../api/client";

describe("MetricsConsole", () => {
  // The proxy answers 403 to this caller; asking anyway would only show errors.
  it("shows a quiet note instead of the console to a caller who may not query", async () => {
    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <MemoryRouter initialEntries={["/metrics?q=up"]}>
          <MetricsConsole />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByText("Metrics aren't available for your access."),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(vi.mocked(api.metricsQuery)).not.toHaveBeenCalled();
    expect(vi.mocked(api.metricsLabels)).not.toHaveBeenCalled();
  });
});
