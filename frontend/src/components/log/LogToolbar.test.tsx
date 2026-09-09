import type { TimeRange } from "./log-utils";
import { TimeRangeSelector } from "./LogToolbar";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";

// The custom inputs are seeded as the panel opens rather than from an effect
// watching `value`. These cover the two halves of that: opening still shows the
// range in force, and a range arriving while the panel is open no longer
// overwrites what someone has typed into it.
describe("TimeRangeSelector", () => {
  const noop = vi.fn<(range: TimeRange) => void>();

  const range = {
    label: "Custom",
    since: "2026-09-09T10:00:00.000Z",
    until: "2026-09-09T11:00:00.000Z",
  };

  function openPanel() {
    return userEvent.click(screen.getByTitle("Time range"));
  }

  it("seeds the custom inputs from the range in force when opened", async () => {
    render(
      <TimeRangeSelector
        value={range}
        onChange={noop}
      />,
    );

    await openPanel();

    const [since, until] = screen.getAllByDisplayValue(/2026-09-09T/);
    expect(since).toBeInTheDocument();
    expect(until).toBeInTheDocument();
  });

  it("re-seeds on each open, so a range changed while closed is picked up", async () => {
    const { rerender } = render(
      <TimeRangeSelector
        value={{ label: "Last hour" }}
        onChange={noop}
      />,
    );

    await openPanel();
    expect(screen.queryByDisplayValue(/2026-09-09T/)).not.toBeInTheDocument();

    await openPanel(); // close
    rerender(
      <TimeRangeSelector
        value={range}
        onChange={noop}
      />,
    );
    await openPanel();

    expect(screen.getAllByDisplayValue(/2026-09-09T/)).toHaveLength(2);
  });

  it("keeps a typed value when the range changes underneath an open panel", async () => {
    const { rerender } = render(
      <TimeRangeSelector
        value={{ label: "Last hour" }}
        onChange={noop}
      />,
    );

    await openPanel();

    const [since] = screen.getAllByDisplayValue("");

    if (!since) {
      throw new Error("the open panel rendered no empty custom-range input");
    }

    await userEvent.type(since, "2026-01-01T09:00");

    rerender(
      <TimeRangeSelector
        value={range}
        onChange={noop}
      />,
    );

    expect(screen.getByDisplayValue("2026-01-01T09:00")).toBeInTheDocument();
  });
});
