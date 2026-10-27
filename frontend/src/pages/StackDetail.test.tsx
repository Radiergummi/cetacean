import { composeQueryKey } from "../components/ComposeSection";
import StackDetail from "./StackDetail";
import { api } from "@/api/client";
import { createTestQueryClient } from "@/test/mocks";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

interface StreamEvent {
  type: string;
  action: string;
  id: string;
}

let subscriber: ((event: StreamEvent) => void) | undefined;

vi.mock("../hooks/useResourceStream", () => ({
  useResourceStream: (_path: string, listener: (event: never) => void) => {
    subscriber = listener as (event: StreamEvent) => void;
  },
}));

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    stack: vi.fn<() => Promise<unknown>>(),
    allServices: vi.fn<() => Promise<unknown>>(),
    allTasks: vi.fn<() => Promise<unknown>>(),
    serviceTasks: vi.fn<() => Promise<unknown>>(),
  },
}));

const stack = {
  name: "web",
  services: [
    {
      ID: "svc1",
      Spec: { Name: "web_api", Mode: { Replicated: { Replicas: 1 } } },
    },
  ],
  configs: [],
  secrets: [],
  networks: [],
  volumes: [],
};

beforeEach(() => {
  subscriber = undefined;
  vi.mocked(api.stack).mockResolvedValue({
    data: { stack },
    allowedMethods: new Set<string>(),
  } as never);
  vi.mocked(api.serviceTasks).mockResolvedValue([] as never);
  vi.mocked(api.allServices).mockResolvedValue([] as never);
  vi.mocked(api.allTasks).mockResolvedValue([] as never);
});

describe("StackDetail", () => {
  // A stack's document is built from its services, networks and volumes, and
  // its stream carries an event for each of them.
  it("invalidates the compose document when the stack changes", async () => {
    const client = createTestQueryClient();
    const invalidate = vi.spyOn(client, "invalidateQueries");

    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/stacks/web"]}>
          <Routes>
            <Route
              path="/stacks/:name"
              element={<StackDetail />}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(subscriber).toBeDefined();
    });
    invalidate.mockClear();

    subscriber?.({ type: "service", action: "update", id: "svc1" });

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: [...composeQueryKey("stack:web")],
      });
    });
  });

  it("shows the stack's links under the title", async () => {
    vi.mocked(api.stack).mockResolvedValue({
      data: {
        stack,
        links: [
          {
            id: "grafana",
            title: "Dashboard",
            href: "https://grafana.example.com",
            services: ["web_api"],
          },
        ],
      },
      allowedMethods: new Set<string>(),
    } as never);

    renderStackDetail();

    const link = await screen.findByRole("link", { name: "Dashboard" });

    expect(link).toHaveAttribute("href", "https://grafana.example.com");
  });

  // An always-present row would add an empty gap under every stack's header.
  it("renders no links row when the stack has no links", async () => {
    const { container } = renderStackDetail();

    await screen.findByRole("heading", { name: "web" });

    const header = container.querySelector("header");

    expect(screen.queryByRole("list", { name: "Links" })).not.toBeInTheDocument();
    expect(Array.from(header?.children ?? []).every((child) => child.hasChildNodes())).toBe(true);
  });

  it("says why the stack failed to load and offers a retry", async () => {
    vi.mocked(api.stack).mockRejectedValue(new Error("stack web is not visible to you"));

    renderStackDetail();

    expect(await screen.findByText("stack web is not visible to you")).toBeInTheDocument();

    vi.mocked(api.stack).mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(api.stack).toHaveBeenCalled());
  });

  // A global service's target is not known to the page, so a crash loop that
  // leaves nothing running must not read as 0/0 and healthy.
  it("shows a global service with nothing running as critical", async () => {
    const global = { ID: "svc2", Spec: { Name: "web_agent", Mode: { Global: {} } } };

    vi.mocked(api.stack).mockResolvedValue({
      data: { stack: { ...stack, services: [global] } },
      allowedMethods: new Set<string>(),
    } as never);
    vi.mocked(api.allServices).mockResolvedValue([{ ...global, RunningTasks: 0 }] as never);

    const { container } = renderStackDetail();

    await waitFor(() => {
      expect(container.querySelector("td [data-health]")).toHaveAttribute(
        "data-health",
        "critical",
      );
    });
    expect(api.allServices).toHaveBeenCalledWith(
      expect.objectContaining({ filter: 'stack == "web"' }),
      expect.anything(),
    );
    expect(api.serviceTasks).not.toHaveBeenCalled();
  });

  it("says why the task counts failed to load and offers a retry", async () => {
    vi.mocked(api.allServices).mockRejectedValue(new Error("services are not visible to you"));

    renderStackDetail();

    expect(await screen.findByText("services are not visible to you")).toBeInTheDocument();

    vi.mocked(api.allServices).mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(api.allServices).toHaveBeenCalled());
  });

  // A job desires no running tasks, so a count alone calls it healthy however
  // its tasks ended.
  it("shows a job whose tasks all failed as critical", async () => {
    const job = { ID: "job1", Spec: { Name: "web_migrate", Mode: { ReplicatedJob: {} } } };

    vi.mocked(api.stack).mockResolvedValue({
      data: { stack: { ...stack, services: [job] } },
      allowedMethods: new Set<string>(),
    } as never);
    vi.mocked(api.allServices).mockResolvedValue([{ ...job, RunningTasks: 0 }] as never);
    vi.mocked(api.allTasks).mockResolvedValue([
      { ID: "t1", ServiceID: "job1", Status: { State: "failed" } },
      { ID: "t2", ServiceID: "job1", Status: { State: "failed" } },
    ] as never);

    const { container } = renderStackDetail();

    await waitFor(() => {
      expect(container.querySelector("td [data-health]")).toHaveAttribute(
        "data-health",
        "critical",
      );
    });
    expect(api.allTasks).toHaveBeenCalledWith(
      expect.objectContaining({ filter: expect.stringContaining('service in ["job1"]') }),
      expect.anything(),
    );
  });

  // Swarm keeps the tasks of earlier runs; a re-run that fails must not read
  // healthy on the strength of the run before it.
  it("judges a job by its current run only", async () => {
    const job = {
      ID: "job1",
      Spec: { Name: "web_migrate", Mode: { ReplicatedJob: {} } },
      JobStatus: { JobIteration: { Index: 9 } },
    };

    vi.mocked(api.stack).mockResolvedValue({
      data: { stack: { ...stack, services: [job] } },
      allowedMethods: new Set<string>(),
    } as never);
    vi.mocked(api.allServices).mockResolvedValue([{ ...job, RunningTasks: 0 }] as never);
    vi.mocked(api.allTasks).mockResolvedValue([
      { ID: "t1", ServiceID: "job1", JobIteration: { Index: 4 }, Status: { State: "complete" } },
      { ID: "t2", ServiceID: "job1", JobIteration: { Index: 9 }, Status: { State: "failed" } },
    ] as never);

    const { container } = renderStackDetail();

    await waitFor(() => {
      expect(container.querySelector("td [data-health]")).toHaveAttribute(
        "data-health",
        "critical",
      );
    });
  });

  // More members than one page of a list holds still each get their count.
  it("counts the tasks of every service in a large stack", async () => {
    const members = Array.from({ length: 60 }, (_, index) => ({
      ID: `svc${index}`,
      Spec: { Name: `web_${index}`, Mode: { Replicated: { Replicas: 2 } } },
    }));

    vi.mocked(api.stack).mockResolvedValue({
      data: { stack: { ...stack, services: members } },
      allowedMethods: new Set<string>(),
    } as never);
    vi.mocked(api.allServices).mockResolvedValue(
      members.map((member) => ({ ...member, RunningTasks: 2 })) as never,
    );

    const { container } = renderStackDetail();

    await waitFor(() => {
      expect(container.querySelectorAll("td [data-health=healthy]")).toHaveLength(60);
    });
  });
});

function renderStackDetail() {
  return render(
    <QueryClientProvider client={createTestQueryClient()}>
      <MemoryRouter initialEntries={["/stacks/web"]}>
        <Routes>
          <Route
            path="/stacks/:name"
            element={<StackDetail />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}
