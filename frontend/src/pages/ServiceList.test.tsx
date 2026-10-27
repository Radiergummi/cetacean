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
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    services: vi.fn<() => void>(),
    allTasks: vi.fn<() => Promise<unknown>>().mockResolvedValue([]),
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
});

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", localStorageStub);
  mockServices.mockReset();
  vi.mocked(api.allTasks).mockClear();
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

  // A job desires nothing running, so its card's dot needs how its tasks ended.
  it("marks the card of a job whose tasks all failed as critical", async () => {
    showCards();

    const job = fakeJob("job1", "migrate");

    mockServices.mockResolvedValue({
      data: { items: [job], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    vi.mocked(api.allTasks).mockResolvedValue([
      { ID: "t1", ServiceID: "job1", Status: { State: "failed" } },
    ] as never);

    render(<ServiceList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByRole("img", { name: "critical" })).toBeInTheDocument();
    });
  });

  // The service stream carries no task events; a job re-run in place that now
  // completes only shows on the task stream.
  it("refreshes a job card when one of its tasks changes", async () => {
    showCards();
    mockServices.mockResolvedValue({
      data: { items: [fakeJob("job1", "migrate")], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    vi.mocked(api.allTasks).mockResolvedValue([
      { ID: "t1", ServiceID: "job1", Status: { State: "failed" } },
    ] as never);

    render(<ServiceList />, { wrapper });

    expect(await screen.findByRole("img", { name: "critical" })).toBeInTheDocument();

    const completed = { ID: "t2", ServiceID: "job1", Status: { State: "complete" } };
    const stream = [...MockEventSource.instances]
      .reverse()
      .find(({ listeners }) => listeners.has("task"));

    vi.mocked(api.allTasks).mockResolvedValue([completed] as never);
    act(() =>
      stream?.simulateEvent("task", {
        type: "task",
        action: "update",
        id: "t2",
        resource: completed,
      }),
    );

    expect(
      await screen.findByRole("img", { name: "healthy" }, { timeout: 3000 }),
    ).toBeInTheDocument();
  });

  // The cards share one question about every job on the page.
  it("asks how every loaded job ended in one request", async () => {
    showCards();
    mockServices.mockResolvedValue({
      data: {
        items: [fakeJob("job1", "migrate"), fakeJob("job2", "seed")],
        total: 2,
        limit: 50,
        offset: 0,
      },
      allowedMethods: new Set(),
    });

    render(<ServiceList />, { wrapper });

    await waitFor(() => expect(api.allTasks).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(api.allTasks).toHaveBeenCalledTimes(1);
    expect(vi.mocked(api.allTasks).mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({ filter: expect.stringContaining('service in ["job1","job2"]') }),
    );
  });
});

function showCards() {
  vi.stubGlobal("localStorage", {
    ...localStorageStub,
    getItem: (key: string) => (key === "viewMode:services" ? "grid" : null),
  });
}

function fakeJob(id: string, name: string): ServiceListItem {
  const job = fakeService(id, name, 0, 0);

  job.Spec.Mode = { ReplicatedJob: {} };

  return job;
}
