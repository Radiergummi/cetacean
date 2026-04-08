import FetchError from "./FetchError";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

describe("FetchError", () => {
  it("renders default message", () => {
    render(<FetchError />);
    expect(screen.getByText("Failed to load data")).toBeInTheDocument();
  });

  it("renders custom message", () => {
    render(<FetchError message="Something broke" />);
    expect(screen.getByText("Something broke")).toBeInTheDocument();
  });

  it("shows retry button when onRetry provided", () => {
    render(<FetchError onRetry={vi.fn<() => void>()} />);
    expect(screen.getByText("Retry")).toBeInTheDocument();
  });

  it("hides retry button when no onRetry", () => {
    render(<FetchError />);
    expect(screen.queryByText("Retry")).not.toBeInTheDocument();
  });

  it("calls onRetry when clicked", () => {
    const onRetry = vi.fn<() => void>();
    render(<FetchError onRetry={onRetry} />);
    fireEvent.click(screen.getByText("Retry"));
    expect(onRetry).toHaveBeenCalledOnce();
  });
});
