import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import TaskStateFilter from "./TaskStateFilter";
import type { Task } from "../api/types";

const fakeTasks: Task[] = [
  { Status: { State: "running" } },
  { Status: { State: "running" } },
  { Status: { State: "failed" } },
] as Task[];

describe("TaskStateFilter", () => {
  it("renders all button with total count", () => {
    render(<TaskStateFilter tasks={fakeTasks} active={null} onChange={() => {}} />);
    expect(screen.getByText("All")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("renders state buttons with counts", () => {
    render(<TaskStateFilter tasks={fakeTasks} active={null} onChange={() => {}} />);
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
  });

  it("calls onChange with state on click", () => {
    const onChange = vi.fn();
    render(<TaskStateFilter tasks={fakeTasks} active={null} onChange={onChange} />);
    fireEvent.click(screen.getByText("Running"));
    expect(onChange).toHaveBeenCalledWith("running");
  });

  it("calls onChange with same state when clicking active state", () => {
    const onChange = vi.fn();
    render(<TaskStateFilter tasks={fakeTasks} active="running" onChange={onChange} />);
    fireEvent.click(screen.getByText("Running"));
    expect(onChange).toHaveBeenCalledWith("running");
  });

  it("calls onChange with null when clicking All", () => {
    const onChange = vi.fn();
    render(<TaskStateFilter tasks={fakeTasks} active="running" onChange={onChange} />);
    fireEvent.click(screen.getByText("All"));
    expect(onChange).toHaveBeenCalledWith(null);
  });
});
