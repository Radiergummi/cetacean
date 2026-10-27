import type { Task } from "../api/types";
import {
  MockEventSource,
  createTestQueryClient,
  createWrapper,
  localStorageStub,
} from "../test/mocks";
import TaskList from "./TaskList";
import { QueryClient } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("../api/client", () => ({
  pageSize: 50,
  emptyMethods: new Set(),
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

  function renderAt(url: string) {
    mockTasks.mockResolvedValue({
      data: { items: [fakeTask(0)], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });

    render(<TaskList />, {
      wrapper: ({ children }: { children: ReactNode }) =>
        createWrapper(testQueryClient, { withRouter: false })({
          children: <MemoryRouter initialEntries={[url]}>{children}</MemoryRouter>,
        }),
    });
  }

  function requestedFilter() {
    const [params] = mockTasks.mock.calls.at(-1) ?? [];

    return params?.filter;
  }

  it("asks the server for the selected state", async () => {
    renderAt("/tasks?state=failed");

    expect(await screen.findByText("Slot 1")).toBeInTheDocument();
    expect(requestedFilter()).toBe('state == "failed"');
  });

  it("places the state filter beneath the search toolbar", async () => {
    renderAt("/tasks");

    const filter = await screen.findByText("Active");
    const search = screen.getByPlaceholderText("Search tasks…");

    expect(search.compareDocumentPosition(filter) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("asks the server for active tasks by default", async () => {
    renderAt("/tasks");

    expect(await screen.findByText("Slot 1")).toBeInTheDocument();
    expect(requestedFilter()).toMatch(/^!\(desired_state == "shutdown" && state in \[/);
  });

  it("follows the stream on the default active filter", async () => {
    const page = (...tasks: Task[]) => ({
      data: { items: tasks, total: tasks.length, limit: 50, offset: 0 },
      allowedMethods: new Set<string>(),
    });
    const failed = {
      ...fakeTask(0),
      DesiredState: "shutdown",
      Status: { State: "failed", Timestamp: "2026-10-09T09:01:00Z" },
    } as unknown as Task;

    renderAt("/tasks");
    expect(await screen.findByText("Slot 1")).toBeInTheDocument();

    const stream = [...MockEventSource.instances]
      .reverse()
      .find(({ listeners }) => listeners.has("task"));
    const send = (action: string, task: Task) =>
      act(() =>
        stream?.simulateEvent("task", { type: "task", action, id: task.ID, resource: task }),
      );

    mockTasks.mockResolvedValue(page(fakeTask(0), fakeTask(1)));
    send("create", fakeTask(1));
    expect(await screen.findByText("Slot 2", {}, { timeout: 3000 })).toBeInTheDocument();

    mockTasks.mockResolvedValue(page(fakeTask(1)));
    send("update", failed);
    await waitFor(() => expect(screen.queryByText("Slot 1")).not.toBeInTheDocument(), {
      timeout: 3000,
    });
    expect(screen.getByText("Slot 2")).toBeInTheDocument();
  });

  it("sends no filter for all tasks", async () => {
    renderAt("/tasks?state=__all__");

    expect(await screen.findByText("Slot 1")).toBeInTheDocument();
    expect(requestedFilter()).toBeUndefined();
  });
});
