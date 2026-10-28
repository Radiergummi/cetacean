import TaskTimeline from "./TaskTimeline";
import type { Task } from "@/api/types";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

const minute = 60_000;

function makeTask(createdAgo: number): Task {
  const createdAt = new Date(Date.now() - createdAgo * minute).toISOString();

  return {
    ID: "task-a",
    Version: { Index: 1 },
    CreatedAt: createdAt,
    ServiceID: "service-a",
    ServiceName: "web",
    NodeID: "node-a",
    NodeHostname: "worker-1",
    Slot: 1,
    Status: { Timestamp: createdAt, State: "running" },
    DesiredState: "running",
    Spec: {},
  };
}

function renderTimeline(variant: "service" | "node") {
  return render(
    <MemoryRouter>
      <TaskTimeline
        tasks={[makeTask(20)]}
        variant={variant}
      />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.useRealTimers();
  localStorage.clear();
});

describe("TaskTimeline", () => {
  it("notes visibly that a node page row had no task on this node before its first bar", () => {
    renderTimeline("node");

    expect(
      screen.getByText("Hatched: no task of that slot on this node before its first bar."),
    ).toBeVisible();
    expect(screen.queryByText(/history/i)).toBeNull();
  });

  it("says that a service page row had no task in its slot before its first bar", () => {
    renderTimeline("service");

    expect(screen.getByText("Hatched: no task in that slot before its first bar.")).toBeVisible();
  });

  it("stops its clock while the section is collapsed", () => {
    vi.useFakeTimers();
    renderTimeline("service");

    const ticking = vi.getTimerCount();

    fireEvent.click(screen.getByRole("button", { name: "Task Timeline" }));

    expect(vi.getTimerCount()).toBe(ticking - 1);
  });
});
