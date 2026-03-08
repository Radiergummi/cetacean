import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import NetworkList from "./NetworkList";
import type { Network } from "../api/types";

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
    networks: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockNetworks = vi.mocked(api.networks);

const fakeNetwork = (id: string, name: string): Network => ({
  Id: id,
  Name: name,
  Driver: "overlay",
  Scope: "swarm",
  IPAM: { Config: [{ Subnet: "10.0.0.0/24", Gateway: "10.0.0.1" }] },
  Labels: {},
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockNetworks.mockReset();
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

describe("NetworkList", () => {
  it("renders network list", async () => {
    mockNetworks.mockResolvedValue({
      items: [fakeNetwork("n1", "ingress"), fakeNetwork("n2", "backend")],
      total: 2,
    });
    render(<NetworkList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("ingress")).toBeInTheDocument();
    });
    expect(screen.getByText("backend")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockNetworks.mockResolvedValue({ items: [], total: 0 });
    render(<NetworkList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No networks found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockNetworks.mockRejectedValue(new Error("Connection refused"));
    render(<NetworkList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Connection refused")).toBeInTheDocument();
    });
  });
});
