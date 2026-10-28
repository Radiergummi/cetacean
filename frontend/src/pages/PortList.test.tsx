import type { PublishedPort } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import PortList, { changesPorts } from "./PortList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  api: {
    ports: vi.fn<() => void>(),
  },
}));

import { api } from "../api/client";
const mockPorts = vi.mocked(api.ports);

const ports: PublishedPort[] = [
  {
    PublishedPort: 443,
    TargetPort: 8443,
    Protocol: "tcp",
    PublishMode: "host",
    ServiceID: "s1",
    ServiceName: "edge",
    Nodes: ["n1", "n2"],
  },
  {
    PublishedPort: 8080,
    TargetPort: 80,
    Protocol: "tcp",
    PublishMode: "ingress",
    ServiceID: "s2",
    ServiceName: "shop_web",
    Stack: "shop",
  },
];

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockPorts.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
}

describe("PortList", () => {
  it("lists each port with the service publishing it and where it is bound", async () => {
    mockPorts.mockResolvedValue({
      data: { items: ports, total: 2, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<PortList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("443/tcp")).toBeInTheDocument();
    });
    expect(screen.getByText("8080/tcp")).toBeInTheDocument();
    expect(screen.getByText("2 nodes")).toBeInTheDocument();
    expect(screen.getByText("every node")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockPorts.mockResolvedValue({
      data: { items: [], total: 0, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<PortList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No service publishes a port")).toBeInTheDocument();
    });
  });
});

describe("changesPorts", () => {
  const task = (mode: string) => ({
    type: "task",
    action: "update",
    id: "t1",
    resource: { Status: { PortStatus: { Ports: [{ PublishMode: mode }] } } },
  });

  it("lets a task holding a host port through", () => {
    expect(changesPorts(task("host"))).toBe(true);
  });

  it("ignores a task that holds no host port", () => {
    expect(changesPorts(task("ingress"))).toBe(false);
    expect(
      changesPorts({ type: "task", action: "update", id: "t2", resource: { Status: {} } }),
    ).toBe(false);
  });

  it("lets every service event and a bare task removal through", () => {
    expect(changesPorts({ type: "service", action: "update", id: "s1", resource: {} })).toBe(true);
    expect(changesPorts({ type: "task", action: "remove", id: "t3" })).toBe(true);
  });
});
