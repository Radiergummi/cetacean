import type { Task } from "../api/types";
import TaskStateFilter from "./TaskStateFilter";
import { render, screen, within, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

const fakeTasks: Task[] = [
  { DesiredState: "running", Status: { State: "running" } },
  { DesiredState: "running", Status: { State: "running" } },
  { DesiredState: "shutdown", Status: { State: "failed" } },
] as Task[];

describe("TaskStateFilter", () => {
  it("renders Active and All segments with counts", () => {
    render(
      <TaskStateFilter
        tasks={fakeTasks}
        active={null}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("All")).toBeInTheDocument();
  });

  it("renders visible state segments with counts", () => {
    render(
      <TaskStateFilter
        tasks={fakeTasks}
        active={null}
        onChange={() => {}}
      />,
    );
    // With max=3, only Active, All, and Running are visible
    const running = screen.getByText("Running").closest("button")!;
    expect(running).toBeInTheDocument();
    expect(within(running).getByText("2")).toBeInTheDocument();
  });

  it("calls onChange with state on click", () => {
    const onChange = vi.fn<(state: string | null) => void>();
    render(
      <TaskStateFilter
        tasks={fakeTasks}
        active={null}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText("Running"));
    expect(onChange).toHaveBeenCalledWith("running");
  });

  it("defaults to Active filter and calls onChange(null) for Active", () => {
    const onChange = vi.fn<(state: string | null) => void>();
    render(
      <TaskStateFilter
        tasks={fakeTasks}
        active="running"
        onChange={onChange}
      />,
    );
    // Clicking Active maps __active__ → null
    fireEvent.click(screen.getByText("Active"));
    expect(onChange).toHaveBeenCalledWith(null);
  });

  it("counts only the selected segment when the server filtered", () => {
    render(
      <TaskStateFilter
        tasks={fakeTasks}
        total={7}
        active={null}
        onChange={() => {}}
      />,
    );

    const activeSegment = screen.getByText("Active").closest("button")!;
    const allSegment = screen.getByText("All").closest("button")!;

    expect(within(activeSegment).getByText("7")).toBeInTheDocument();
    expect(allSegment.textContent).toBe("All");
    expect(screen.getByText("Running").closest("button")).not.toBeDisabled();
  });

  it("keeps a selected state the loaded rows do not hold when the server filtered", () => {
    const onChange = vi.fn<(state: string | null) => void>();
    render(
      <TaskStateFilter
        tasks={[]}
        total={0}
        active="failed"
        onChange={onChange}
      />,
    );

    expect(onChange).not.toHaveBeenCalled();
  });

  it("passes active filter as selected value", () => {
    render(
      <TaskStateFilter
        tasks={fakeTasks}
        active="running"
        onChange={() => {}}
      />,
    );
    // The Running toggle should have data-pressed when active
    const running = screen.getByText("Running").closest("button");
    expect(running).toHaveAttribute("data-pressed");
  });
});
