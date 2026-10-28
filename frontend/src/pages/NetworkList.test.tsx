import type { Network } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import NetworkList, { movesAddresses } from "./NetworkList";
import { QueryClient } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
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
              taskAddresses: 200,
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

  it("refetches the figures when a task attached to a network changes", async () => {
    const withUse = (used: number) => ({
      data: {
        items: [
          {
            ...fakeNetwork("n1", "backend"),
            Addresses: {
              usable: 253,
              used,
              vips: 1,
              taskAddresses: used - 2,
              loadBalancers: 1,
              excludesAttachedContainers: false,
            },
          },
        ],
        total: 1,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set<string>(),
    });

    mockNetworks.mockResolvedValue(withUse(3));
    render(<NetworkList />, { wrapper });
    expect(await screen.findByText("3 / 253")).toBeInTheDocument();

    const stream = [...MockEventSource.instances]
      .reverse()
      .find(({ listeners }) => listeners.has("task"));

    mockNetworks.mockResolvedValue(withUse(4));
    act(() =>
      stream?.simulateEvent("task", {
        type: "task",
        action: "create",
        id: "t1",
        resource: { ID: "t1", NetworksAttachments: [{ Network: { ID: "n1" } }] },
      }),
    );

    expect(await screen.findByText("4 / 253", {}, { timeout: 3000 })).toBeInTheDocument();
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

describe("movesAddresses", () => {
  it("lets a node event through only while ingress is listed", () => {
    const node = { type: "node", action: "create", id: "n1", resource: {} };
    const ingress = { ...fakeNetwork("n2", "ingress"), Ingress: true };

    expect(movesAddresses(node, [fakeNetwork("n1", "backend")])).toBe(false);
    expect(movesAddresses(node, [fakeNetwork("n1", "backend"), ingress])).toBe(true);
  });

  const loaded = [fakeNetwork("n1", "backend")];
  const task = (networkID: string) => ({
    type: "task",
    action: "update",
    id: "t1",
    resource: { NetworksAttachments: [{ Network: { ID: networkID } }] },
  });

  it("lets a task attached to a listed network through", () => {
    expect(movesAddresses(task("n1"), loaded)).toBe(true);
  });

  it("ignores a task on a network the list doesn't hold, or on none", () => {
    expect(movesAddresses(task("n9"), loaded)).toBe(false);
    expect(movesAddresses({ type: "task", action: "update", id: "t2", resource: {} }, loaded)).toBe(
      false,
    );
  });

  it("lets a service event and a bare task removal through", () => {
    expect(
      movesAddresses({ type: "service", action: "update", id: "s1", resource: {} }, loaded),
    ).toBe(true);
    expect(movesAddresses({ type: "task", action: "remove", id: "t3" }, loaded)).toBe(true);
  });
});
