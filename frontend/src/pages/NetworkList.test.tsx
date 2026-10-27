import type { Network } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import NetworkList from "./NetworkList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    networks: vi.fn<() => void>(),
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

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockNetworks.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
}

describe("NetworkList", () => {
  it("renders network list", async () => {
    mockNetworks.mockResolvedValue({
      data: {
        items: [fakeNetwork("n1", "ingress"), fakeNetwork("n2", "backend")],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<NetworkList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("ingress")).toBeInTheDocument();
    });
    expect(screen.getByText("backend")).toBeInTheDocument();
  });

  it("shows used against usable addresses, toned at the warning threshold", async () => {
    mockNetworks.mockResolvedValue({
      data: {
        items: [
          {
            ...fakeNetwork("n1", "backend"),
            Addresses: {
              usable: 253,
              used: 210,
              vips: 4,
              tasks: 200,
              loadBalancers: 6,
              excludesAttachedContainers: false,
            },
          },
          fakeNetwork("n2", "bridge"),
        ],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });
    render(<NetworkList />, { wrapper });

    const use = await screen.findByText("210 / 253");
    expect(use).toHaveClass("text-status-warning");
    expect(screen.getByText("\u2014")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockNetworks.mockResolvedValue({
      data: { items: [], total: 0, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
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
