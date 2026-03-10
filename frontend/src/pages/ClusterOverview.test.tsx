import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import ClusterOverview from "./ClusterOverview";

// Mock EventSource
class MockEventSource {
  static instance: MockEventSource;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;
  constructor(_url: string) {
    MockEventSource.instance = this;
  }
  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }
  close() {
    this.closed = true;
  }
}

vi.mock("../api/client", () => ({
  api: {
    cluster: vi.fn(),
    history: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("../components/PrometheusBanner", () => ({
  default: () => null,
}));

vi.mock("../components/CapacitySection", () => ({
  default: () => <div data-testid="capacity-section" />,
}));

vi.mock("../components/ActivityFeed", () => ({
  default: () => <div data-testid="activity-feed" />,
}));

import { api } from "../api/client";
const mockCluster = vi.mocked(api.cluster);

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  mockCluster.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <SSEProvider>{children}</SSEProvider>
    </MemoryRouter>
  );
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
