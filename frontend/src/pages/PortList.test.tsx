import type { PublishedPort } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import PortList from "./PortList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    ports: vi.fn<() => void>(),
  },
}));

import { api } from "../api/client";
const mockPorts = vi.mocked(api.ports);

const ports: PublishedPort[] = [
  {
    publishedPort: 443,
    targetPort: 8443,
    protocol: "tcp",
    publishMode: "host",
    serviceId: "s1",
    serviceName: "edge",
    nodes: ["n1", "n2"],
  },
  {
    publishedPort: 8080,
    targetPort: 80,
    protocol: "tcp",
    publishMode: "ingress",
    serviceId: "s2",
    serviceName: "shop_web",
    stack: "shop",
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
