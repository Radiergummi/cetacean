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
    notificationRules: vi.fn().mockResolvedValue([]),
  },
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
    });

    render(<ClusterOverview />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Nodes Ready")).toBeInTheDocument();
    });
    expect(screen.getByText("Services")).toBeInTheDocument();
    expect(screen.getByText("Stacks")).toBeInTheDocument();
    expect(screen.getByText("Tasks Running")).toBeInTheDocument();
    expect(screen.getByText("Tasks Failed")).toBeInTheDocument();
    expect(screen.getByText("12")).toBeInTheDocument(); // serviceCount
    expect(screen.getByText("8")).toBeInTheDocument(); // stackCount
    expect(screen.getByText("39")).toBeInTheDocument(); // tasks running
    expect(screen.getByText("47")).toBeInTheDocument(); // task total
  });
});
