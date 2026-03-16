import TaskStatusBadge from "./TaskStatusBadge";
import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

describe("TaskStatusBadge", () => {
  it("renders state text", () => {
    render(<TaskStatusBadge state="running" />);
    expect(screen.getByText("running")).toBeInTheDocument();
  });

  it("renders 'unknown' when no state", () => {
    render(<TaskStatusBadge />);
    expect(screen.getByText("unknown")).toBeInTheDocument();
  });

  it("applies green classes for running", () => {
    render(<TaskStatusBadge state="running" />);
    expect(screen.getByText("running").className).toContain("bg-green");
  });

  it("applies red classes for failed", () => {
    render(<TaskStatusBadge state="failed" />);
    expect(screen.getByText("failed").className).toContain("bg-red");
  });

  it("applies yellow classes for preparing", () => {
    render(<TaskStatusBadge state="preparing" />);
    expect(screen.getByText("preparing").className).toContain("bg-yellow");
  });

  it("applies gray classes for shutdown", () => {
    render(<TaskStatusBadge state="shutdown" />);
    expect(screen.getByText("shutdown").className).toContain("bg-gray");
  });

  it("applies fallback classes for unknown state", () => {
    render(<TaskStatusBadge state="foobar" />);
    expect(screen.getByText("foobar").className).toContain("bg-gray");
  });
});
