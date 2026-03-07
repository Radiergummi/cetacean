import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ViewToggle from "./ViewToggle";

describe("ViewToggle", () => {
  it("renders table and grid buttons", () => {
    render(<ViewToggle mode="table" onChange={vi.fn()} />);
    expect(screen.getByTitle("Table view")).toBeInTheDocument();
    expect(screen.getByTitle("Grid view")).toBeInTheDocument();
  });

  it("calls onChange with 'grid' when grid clicked", () => {
    const onChange = vi.fn();
    render(<ViewToggle mode="table" onChange={onChange} />);
    fireEvent.click(screen.getByTitle("Grid view"));
    expect(onChange).toHaveBeenCalledWith("grid");
  });

  it("calls onChange with 'table' when table clicked", () => {
    const onChange = vi.fn();
    render(<ViewToggle mode="grid" onChange={onChange} />);
    fireEvent.click(screen.getByTitle("Table view"));
    expect(onChange).toHaveBeenCalledWith("table");
  });

  it("highlights active mode", () => {
    render(<ViewToggle mode="table" onChange={vi.fn()} />);
    expect(screen.getByTitle("Table view").className).toContain("bg-muted");
    expect(screen.getByTitle("Grid view").className).not.toContain("bg-muted text-foreground");
  });
});
