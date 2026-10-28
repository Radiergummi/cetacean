import type { ServiceListItem } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import ServiceList from "./ServiceList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen, waitFor, fireEvent, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  api: {
    services: vi.fn<() => void>(),
    monitoringStatus: vi.fn<() => Promise<unknown>>().mockResolvedValue(null),
  },
}));

import { api } from "../api/client";
const mockServices = vi.mocked(api.services);

const fakeService = (id: string, name: string, replicas = 3, running = 3): ServiceListItem => ({
  ID: id,
  Version: { Index: 1 },
  Spec: {
    Name: name,
    Labels: {},
    TaskTemplate: { ContainerSpec: { Image: "nginx:latest@sha256:abc" } },
    Mode: { Replicated: { Replicas: replicas } },
  },
  RunningTasks: running,
  DesiredTasks: replicas,
});

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockServices.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
}

describe("ServiceList", () => {
  it("renders service list with replica health", async () => {
    const items = [fakeService("s1", "web", 3, 3), fakeService("s2", "api", 2, 1)];
    mockServices.mockResolvedValue({
      data: { items, total: 2, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("web")).toBeInTheDocument();
    });
    expect(screen.getByText("api")).toBeInTheDocument();
    // Healthy: 3/3
    expect(screen.getByText("3/3")).toBeInTheDocument();
    // Unhealthy: 1/2
    expect(screen.getByText("1/2")).toBeInTheDocument();
  });

  it("filters by search with debounce", async () => {
    vi.useFakeTimers();
    mockServices
      .mockResolvedValueOnce({
        data: {
          items: [fakeService("s1", "web-frontend"), fakeService("s2", "api-backend")],
          total: 2,
          limit: 50,
          offset: 0,
        },
        allowedMethods: new Set(),
      })
      .mockResolvedValueOnce({
        data: { items: [fakeService("s2", "api-backend")], total: 1, limit: 50, offset: 0 },
        allowedMethods: new Set(),
      });
    render(<ServiceList />, { wrapper });

    await vi.waitFor(() => {
      expect(screen.getByText("web-frontend")).toBeInTheDocument();
    });

    fireEvent.change(screen.getByPlaceholderText("Search services…"), {
      target: { value: "api" },
    });

    // Advance past debounce
    await act(async () => {
      vi.advanceTimersByTime(300);
    });

    await vi.waitFor(() => {
      expect(screen.queryByText("web-frontend")).not.toBeInTheDocument();
    });
    expect(screen.getByText("api-backend")).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("marks a service whose image tag names a newer image", async () => {
    const moved = {
      ...fakeService("s1", "web"),
      ImageUpdate: {
        image: "nginx:latest",
        currentDigest: "sha256:abc",
        latestDigest: "sha256:def",
        suggestedImage: "nginx:latest@sha256:def",
        checkedAt: "2026-10-01T12:00:00Z",
      },
    };

    mockServices.mockResolvedValue({
      data: { items: [moved, fakeService("s2", "api")], total: 2, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("web")).toBeInTheDocument();
    });
    expect(screen.getAllByText("Newer image")).toHaveLength(1);
  });

  it("shows empty state", async () => {
    mockServices.mockResolvedValue({
      data: { items: [], total: 0, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No services found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockServices.mockRejectedValue(new Error("Server error"));
    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Server error")).toBeInTheDocument();
    });
  });

  it("judges a global service against its eligible nodes", async () => {
    showCards();
    mockServices.mockResolvedValue({
      data: { items: [fakeGlobal("g1", "agent", 3, 2)], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });

    render(<ServiceList />, { wrapper });

    expect(await screen.findByRole("img", { name: "warning" })).toBeInTheDocument();
  });

  it("shows a global service's running count against its eligible nodes", async () => {
    mockServices.mockResolvedValue({
      data: { items: [fakeGlobal("g1", "agent", 3, 2)], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });

    render(<ServiceList />, { wrapper });

    expect(await screen.findByText("2/3")).toBeInTheDocument();
  });

  it("marks the card of a job whose tasks all failed as critical", async () => {
    showCards();
    mockServices.mockResolvedValue({
      data: { items: [fakeJob("job1", "migrate", 0, 3)], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });

    render(<ServiceList />, { wrapper });

    expect(await screen.findByRole("img", { name: "critical" })).toBeInTheDocument();
  });

  // The services stream carries task events, so no second stream is opened.
  it("refetches when a task of a listed service changes", async () => {
    mockServices.mockResolvedValue({
      data: { items: [fakeService("s1", "web", 3, 1)], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });

    render(<ServiceList />, { wrapper });

    expect(await screen.findByText("1/3")).toBeInTheDocument();

    mockServices.mockResolvedValue({
      data: { items: [fakeService("s1", "web", 3, 3)], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    act(() =>
      servicesStream()?.simulateEvent("task", {
        type: "task",
        action: "update",
        id: "t1",
        resource: { ID: "t1", ServiceID: "s1", Status: { State: "running" } },
      }),
    );

    expect(await screen.findByText("3/3", undefined, { timeout: 3000 })).toBeInTheDocument();
    expect(screen.queryByText("t1")).not.toBeInTheDocument();
    expect(MockEventSource.instances.filter(({ url }) => url.includes("/tasks"))).toHaveLength(0);
  });

  it("leaves the list alone when a task of another service changes", async () => {
    vi.useFakeTimers();
    mockServices.mockResolvedValue({
      data: { items: [fakeService("s1", "web")], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });

    render(<ServiceList />, { wrapper });

    await vi.waitFor(() => {
      expect(screen.getByText("web")).toBeInTheDocument();
    });

    const calls = mockServices.mock.calls.length;

    act(() =>
      servicesStream()?.simulateEvent("task", {
        type: "task",
        action: "create",
        id: "t9",
        resource: { ID: "t9", ServiceID: "elsewhere", Status: { State: "running" } },
      }),
    );

    await act(async () => {
      vi.advanceTimersByTime(2000);
    });

    expect(mockServices.mock.calls.length).toBe(calls);
    vi.useRealTimers();
  });
});

function servicesStream() {
  return [...MockEventSource.instances].reverse().find(({ url }) => url.endsWith("/services"));
}

function showCards() {
  vi.stubGlobal("localStorage", {
    ...localStorageStub,
    getItem: (key: string) => (key === "viewMode:services" ? "grid" : null),
  });
}

function fakeJob(id: string, name: string, completed = 0, failed = 0): ServiceListItem {
  const job = fakeService(id, name, 0, 0);

  job.Spec.Mode = { ReplicatedJob: {} };
  job.DesiredTasks = 1;
  job.CompletedTasks = completed;
  job.FailedTasks = failed;

  return job;
}

function fakeGlobal(id: string, name: string, eligible: number, running: number): ServiceListItem {
  const service = fakeService(id, name, 0, running);

  service.Spec.Mode = { Global: {} };
  service.DesiredTasks = eligible;

  return service;
}
