import { MockEventSource, createTestQueryClient, createWrapper } from "../test/mocks";
import ClusterOverview from "./ClusterOverview";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// Mock EventSource
vi.mock("../api/client", () => ({
  api: {
    cluster: vi.fn<() => void>(),
    history: vi.fn<() => Promise<unknown>>().mockResolvedValue([]),
    diskUsage: vi.fn<() => Promise<unknown>>().mockResolvedValue([]),
    monitoringStatus: vi.fn<() => Promise<unknown>>().mockResolvedValue({
      prometheusConfigured: false,
      prometheusReachable: false,
      nodeExporter: null,
      cadvisor: null,
    }),
  },
}));

vi.mock("../components/metrics", () => ({
  MonitoringStatus: () => null,
  CapacitySection: () => <div data-testid="capacity-section" />,
}));

vi.mock("../components/metrics/MetricsPanel", () => ({
  default: () => <div data-testid="metrics-panel" />,
}));

vi.mock("../components/ActivityFeed", () => ({
  default: () => <div data-testid="activity-feed" />,
}));

import { api } from "../api/client";
const mockCluster = vi.mocked(api.cluster);

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  mockCluster.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
}

describe("ClusterOverview", () => {
  it("shows loading skeleton initially", () => {
    mockCluster.mockReturnValue(new Promise(() => {})); // never resolves
    render(<ClusterOverview />, { wrapper });
    expect(screen.getByText("Cluster Overview")).toBeInTheDocument();
  });

  it("renders snapshot data", async () => {
    mockCluster.mockResolvedValue({
      nodeCount: 3,
      serviceCount: 12,
      taskCount: 47,
      stackCount: 8,
      tasksByState: { running: 39, failed: 2 },
      nodesReady: 3,
      nodesDown: 0,
      nodesDraining: 0,
      servicesConverged: 12,
      servicesDegraded: 0,
      reservedCPU: 0,
      reservedMemory: 0,
      totalCPU: 8,
      totalMemory: 17179869184,
      prometheusConfigured: true,
    });

    render(<ClusterOverview />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Nodes")).toBeInTheDocument();
    });
    expect(screen.getByText("Services")).toBeInTheDocument();
    expect(screen.getByText("Failed Tasks")).toBeInTheDocument();
    expect(screen.getByText("Tasks")).toBeInTheDocument();
    // HealthCard primary values
    expect(screen.getByText("3/3 ready")).toBeInTheDocument();
    expect(screen.getByText("12/12 converged")).toBeInTheDocument();
    expect(screen.getByText("39 running")).toBeInTheDocument();
    expect(screen.getByText("47 total · 8 stacks")).toBeInTheDocument();
  });
});
