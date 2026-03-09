import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import NodeList from "./NodeList";
import type { Node } from "../api/types";
import { _resetPrometheusCache } from "../hooks/usePrometheusConfigured";

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
    nodes: vi.fn(),
    cluster: vi.fn().mockResolvedValue({ prometheusConfigured: false }),
    metricsQuery: vi.fn().mockResolvedValue({ data: { result: [] } }),
    metricsQueryRange: vi.fn().mockResolvedValue({ data: { result: [] } }),
  },
}));

import { api } from "../api/client";
const mockNodes = vi.mocked(api.nodes);

const fakeNode = (id: string, hostname: string): Node => ({
  ID: id,
  Version: { Index: 1 },
  Spec: { Role: "worker", Availability: "active", Labels: {} },
  Description: {
    Hostname: hostname,
    Platform: { Architecture: "x86_64", OS: "linux" },
    Resources: { NanoCPUs: 4e9, MemoryBytes: 8e9 },
    Engine: { EngineVersion: "24.0.0" },
  },
  Status: { State: "ready", Addr: "10.0.0.1" },
});

beforeEach(() => {
  _resetPrometheusCache();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockNodes.mockReset();
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

describe("NodeList", () => {
  it("shows loading skeleton initially", () => {
    mockNodes.mockReturnValue(new Promise(() => {}));
    const { container } = render(<NodeList />, { wrapper });
    expect(container.querySelector(".animate-pulse")).toBeInTheDocument();
  });

  it("renders node list", async () => {
    const items = [fakeNode("n1", "node-alpha"), fakeNode("n2", "node-beta")];
    mockNodes.mockResolvedValue({ items, total: 2 });
    render(<NodeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("node-alpha")).toBeInTheDocument();
    });
    expect(screen.getByText("node-beta")).toBeInTheDocument();
  });

  it("filters by search", async () => {
    mockNodes
      .mockResolvedValueOnce({
        items: [fakeNode("n1", "node-alpha"), fakeNode("n2", "node-beta")],
        total: 2,
      })
      .mockResolvedValueOnce({
        items: [fakeNode("n2", "node-beta")],
        total: 1,
      });
    render(<NodeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("node-alpha")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search nodes..."), {
      target: { value: "beta" },
    });

    await waitFor(() => {
      expect(screen.queryByText("node-alpha")).not.toBeInTheDocument();
    });
    expect(screen.getByText("node-beta")).toBeInTheDocument();
  });

  it("shows empty state when no results", async () => {
    mockNodes.mockResolvedValue({ items: [], total: 0 });
    render(<NodeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No nodes found")).toBeInTheDocument();
    });
  });

  it("shows error state on fetch failure", async () => {
    mockNodes.mockRejectedValue(new Error("Network error"));
    render(<NodeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Network error")).toBeInTheDocument();
    });
    expect(screen.getByText("Retry")).toBeInTheDocument();
  });

  it("shows search empty state", async () => {
    mockNodes
      .mockResolvedValueOnce({ items: [fakeNode("n1", "node-alpha")], total: 1 })
      .mockResolvedValueOnce({ items: [], total: 0 });
    render(<NodeList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("node-alpha")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search nodes..."), {
      target: { value: "nonexistent" },
    });

    await waitFor(() => {
      expect(screen.getByText("No nodes match your search")).toBeInTheDocument();
    });
  });
});
