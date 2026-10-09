import type { Task } from "../api/types";
import TasksTable from "./TasksTable";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

const failedTask = (id: string) =>
  ({
    ID: id,
    DesiredState: "shutdown",
    NodeID: "node1",
    ServiceID: "svc1",
    Slot: 1,
    Spec: { ContainerSpec: { Image: "nginx:1.27" } },
    Status: { State: "failed", Timestamp: "2026-10-09T09:00:00Z", Err: "exit 1" },
  }) as unknown as Task;

describe("TasksTable", () => {
  // Between restarts of a crash-looping service every task is terminal, and
  // the default filter hides them all.
  it("says why the default view is empty and offers every task", () => {
    render(
      <MemoryRouter>
        <TasksTable
          tasks={[failedTask("t1"), failedTask("t2")]}
          variant="service"
        />
      </MemoryRouter>,
    );

    expect(screen.getByText(/No active tasks; 2 failed\./)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show all tasks" }));

    expect(screen.getAllByText("exit 1")).toHaveLength(2);
  });
});
