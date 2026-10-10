import type { Task } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import TaskList from "./TaskList";
import { QueryClient } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
  setsEqual: (a: Set<string>, b: Set<string>) => a.size === b.size && [...a].every((x) => b.has(x)),
  api: {
    tasks: vi.fn<() => void>(),
    monitoringStatus: vi.fn<() => Promise<unknown>>().mockResolvedValue(null),
  },
}));

import { api } from "../api/client";
const mockTasks = vi.mocked(api.tasks);

const fakeTask = (index: number) =>
  ({
    ID: `t${index}`,
    DesiredState: "running",
    NodeID: "node1",
    ServiceID: "svc1",
    ServiceName: "web",
    Slot: index + 1,
    Spec: { ContainerSpec: { Image: "nginx:1.27" } },
    Status: { State: "running", Timestamp: "2026-10-09T09:00:00Z" },
  }) as unknown as Task;

let testQueryClient: QueryClient;

beforeEach(() => {
  testQueryClient = createTestQueryClient();
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    ...localStorageStub,
    getItem: (key: string) => (key === "viewMode:tasks" ? "grid" : null),
  });
  mockTasks.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient)({ children });
}

describe("TaskList", () => {
  it("loads the next page from grid view", async () => {
    const page = (offset: number) =>
      Array.from({ length: 50 }, (_, index) => fakeTask(offset + index));
    mockTasks
      .mockResolvedValueOnce({
        data: { items: page(0), total: 100, limit: 50, offset: 0 },
        allowedMethods: new Set(),
      })
      .mockResolvedValueOnce({
        data: { items: page(50), total: 100, limit: 50, offset: 50 },
        allowedMethods: new Set(),
      });
    render(<TaskList />, { wrapper });

    expect(await screen.findByTestId("load-more-sentinel")).toBeInTheDocument();

    const observer = vi.mocked(IntersectionObserver);
    const [callback] = observer.mock.calls.at(-1) as unknown as [IntersectionObserverCallback];
    callback([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver);

    expect(await screen.findByText("Slot 100")).toBeInTheDocument();
  });
});
