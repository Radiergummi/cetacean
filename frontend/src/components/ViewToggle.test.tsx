import ViewToggle from "./ViewToggle";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

describe("ViewToggle", () => {
  it("renders table and grid buttons", () => {
    render(
      <ViewToggle
        mode="table"
        onChange={vi.fn<(mode: string) => void>()}
      />,
    );
    expect(screen.getByLabelText("Table view")).toBeInTheDocument();
    expect(screen.getByLabelText("Grid view")).toBeInTheDocument();
  });

  it("calls onChange with 'grid' when grid clicked", () => {
    const onChange = vi.fn<(mode: string) => void>();
    render(
      <ViewToggle
        mode="table"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByLabelText("Grid view"));
    expect(onChange).toHaveBeenCalledWith("grid");
  });

  it("calls onChange with 'table' when table clicked", () => {
    const onChange = vi.fn<(mode: string) => void>();
    render(
      <ViewToggle
        mode="grid"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByLabelText("Table view"));
    expect(onChange).toHaveBeenCalledWith("table");
  });

  it("highlights active mode", () => {
    render(
      <ViewToggle
        mode="table"
        onChange={vi.fn<(mode: string) => void>()}
      />,
    );
    expect(screen.getByLabelText("Table view")).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByLabelText("Grid view")).toHaveAttribute("aria-pressed", "false");
  });
});
