import type { Task } from "../api/types";
import TasksSection from "./TasksSection";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: { swarm: () => new Promise(() => {}) },
}));

const minute = 60_000;

function makeTask(id: string, state: string, stoppedAgo: number): Task {
  const timestamp = new Date(Date.now() - stoppedAgo * minute).toISOString();

  return {
    ID: id,
    Version: { Index: 1 },
    CreatedAt: new Date(Date.now() - (stoppedAgo + 1) * minute).toISOString(),
    DesiredState: "shutdown",
    NodeID: "node1",
    ServiceID: "svc1",
    Slot: 1,
    Spec: { ContainerSpec: { Image: "nginx:1.27" } },
    Status: { State: state, Timestamp: timestamp, Err: "exit 1" },
  } as unknown as Task;
}

function LocationProbe() {
  return <output aria-label="location">{useLocation().search}</output>;
}

function currentParams() {
  return new URLSearchParams(screen.getByRole("status", { name: "location" }).textContent ?? "");
}

function renderSection(tasks: Task[], url = "/services/svc1") {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[url]}>
        <TasksSection
          tasks={tasks}
          variant="service"
        />
        <LocationProbe />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("TasksSection", () => {
  // Between restarts of a crash-looping service every task is terminal, and
  // the default filter hides them all.
  it("says why the default list is empty and offers every task", () => {
    renderSection([makeTask("t1", "failed", 5), makeTask("t2", "failed", 10)]);

    expect(screen.getByText(/No active tasks; 2 failed\./)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show all tasks" }));

    expect(screen.getAllByText("exit 1")).toHaveLength(2);
  });

  it("lists only the tasks alive inside the page's time range", () => {
    renderSection([makeTask("recent", "failed", 5), makeTask("old", "failed", 120)], "/?range=1h");

    fireEvent.click(screen.getByRole("button", { name: "Show all tasks" }));

    expect(screen.getAllByText("exit 1")).toHaveLength(1);
  });

  it("keeps its time range apart from the charts' range", () => {
    renderSection([makeTask("old", "failed", 120)], "/?range=24h");

    fireEvent.click(screen.getByRole("button", { name: "Show all tasks" }));

    expect(screen.queryByText("exit 1")).toBeNull();
  });

  it("switches to the timeline through the URL and drops the state filter", () => {
    renderSection([makeTask("t1", "failed", 5)]);

    expect(screen.getByRole("button", { name: /Active/ })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Timeline" }));

    expect(currentParams().get("taskView")).toBe("timeline");
    expect(screen.queryByRole("button", { name: /Active/ })).toBeNull();
  });

  it("opens on the timeline a link names", () => {
    renderSection([makeTask("t1", "failed", 5)], "/services/svc1?taskView=timeline");

    expect(screen.getByRole("link", { name: /Task t1, failed/ })).toBeInTheDocument();
  });

  it("stops its clock while the section is collapsed", () => {
    const started = vi.spyOn(window, "setInterval");
    const stopped = vi.spyOn(window, "clearInterval");

    renderSection([makeTask("t1", "failed", 5)]);

    const clocks = started.mock.results.map(({ value }) => value as unknown);

    expect(clocks.length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "Tasks" }));

    expect(stopped.mock.calls.map(([id]) => id as unknown)).toEqual(expect.arrayContaining(clocks));
  });
});
