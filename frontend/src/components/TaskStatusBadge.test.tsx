import TaskStatusBadge from "./TaskStatusBadge";
import { statusTone } from "@/lib/statusColor";
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

  // The badge must not carry its own opinion about what a state means — that
  // mapping belongs to lib/statusColor, which the status dot reads too.
  it.each([
    ["running", "ok"],
    ["failed", "danger"],
    ["preparing", "warning"],
    ["shutdown", "neutral"],
    ["foobar", "neutral"],
  ])("renders %s with the %s tone token", (state, tone) => {
    render(<TaskStatusBadge state={state} />);

    const className = screen.getByText(state).className;

    expect(statusTone(state)).toBe(tone);
    expect(className).toContain(`bg-status-${tone}`);
    expect(className).toContain(`text-status-${tone}`);
  });

  it("states no palette shade of its own", () => {
    render(<TaskStatusBadge state="running" />);
    expect(screen.getByText("running").className).not.toMatch(/-(green|red|yellow|gray)-\d/);
  });
});
