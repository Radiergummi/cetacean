import SearchInput from "./SearchInput";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

describe("SearchInput", () => {
  it("renders with placeholder", () => {
    render(
      <SearchInput
        value=""
        onChange={vi.fn()}
        placeholder="Search nodes..."
      />,
    );
    expect(screen.getByPlaceholderText("Search nodes...")).toBeInTheDocument();
  });

  it("uses default placeholder", () => {
    render(
      <SearchInput
        value=""
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByPlaceholderText("Search\u2026")).toBeInTheDocument();
  });

  it("calls onChange on input", () => {
    const onChange = vi.fn();
    render(
      <SearchInput
        value=""
        onChange={onChange}
      />,
    );
    fireEvent.change(screen.getByPlaceholderText("Search\u2026"), { target: { value: "hello" } });
    expect(onChange).toHaveBeenCalledWith("hello");
  });

  it("shows clear button when value is set", () => {
    render(
      <SearchInput
        value="test"
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole("button")).toBeInTheDocument();
  });

  it("hides clear button when value is empty", () => {
    render(
      <SearchInput
        value=""
        onChange={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("calls onChange with empty string on clear", () => {
    const onChange = vi.fn();
    render(
      <SearchInput
        value="test"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(onChange).toHaveBeenCalledWith("");
  });
});
