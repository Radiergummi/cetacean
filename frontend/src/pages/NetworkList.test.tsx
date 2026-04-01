import type { Network } from "../api/types";
import NetworkList from "./NetworkList";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

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
  pageSize: 50,
  api: {
    networks: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockNetworks = vi.mocked(api.networks);

const fakeNetwork = (id: string, name: string): Network => ({
  Id: id,
  Name: name,
  Created: "2024-01-01T00:00:00Z",
  Driver: "overlay",
  Scope: "swarm",
  EnableIPv6: false,
  Internal: false,
  Attachable: false,
  Ingress: false,
  IPAM: { Config: [{ Subnet: "10.0.0.0/24", Gateway: "10.0.0.1" }] },
  Options: {},
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
      <>{children}</>
    </MemoryRouter>
  );
}

describe("NetworkList", () => {
  it("renders network list", async () => {
    mockNetworks.mockResolvedValue({
      items: [fakeNetwork("n1", "ingress"), fakeNetwork("n2", "backend")],
      total: 2,
      limit: 50,
      offset: 0,
    });
    render(<NetworkList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("ingress")).toBeInTheDocument();
    });
    expect(screen.getByText("backend")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockNetworks.mockResolvedValue({ items: [], total: 0, limit: 50, offset: 0 });
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
