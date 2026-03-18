import { ResourceRangeSlider, computeTicks } from "./resource-range-slider";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

describe("ResourceRangeSlider", () => {
  const defaultProps = {
    label: "CPU (cores)",
    reservation: undefined as number | undefined,
    limit: undefined as number | undefined,
    onChange: vi.fn(),
    max: 4,
    step: 0.25,
  };

  it("renders label", () => {
    render(<ResourceRangeSlider {...defaultProps} />);
    expect(screen.getByText("CPU (cores)")).toBeInTheDocument();
  });

  it("shows dashes when both values are undefined", () => {
    render(<ResourceRangeSlider {...defaultProps} />);
    const dashes = screen.getAllByText("—");
    expect(dashes).toHaveLength(2);
  });

  it("shows reservation input when reservation is set", () => {
    render(
      <ResourceRangeSlider
        {...defaultProps}
        reservation={0.5}
      />,
    );
    const input = screen.getAllByRole("textbox")[0];
    expect(input).toHaveValue("0.5");
  });

  it("shows limit input when limit is set", () => {
    render(
      <ResourceRangeSlider
        {...defaultProps}
        limit={2}
      />,
    );
    const inputs = screen.getAllByRole("textbox");
    expect(inputs[inputs.length - 1]).toHaveValue("2");
  });

  it("renders Reserved and Limit labels", () => {
    render(<ResourceRangeSlider {...defaultProps} />);
    expect(screen.getByText("Reserved")).toBeInTheDocument();
    expect(screen.getByText("Limit")).toBeInTheDocument();
  });
});

describe("computeTicks", () => {
  it("produces boundary ticks at step and max for CPU", () => {
    const ticks = computeTicks(4, 0.25);
    expect(ticks[0]).toMatchObject({ value: 0.25, tall: true });
    expect(ticks[ticks.length - 1]).toMatchObject({ value: 4, tall: true });
  });

  it("produces intermediate ticks at whole cores", () => {
    const ticks = computeTicks(4, 0.25);
    const intermediates = ticks.filter((t) => !t.tall);
    expect(intermediates.map((t) => t.value)).toEqual([1, 2, 3]);
  });

  it("produces boundary ticks for memory", () => {
    const ticks = computeTicks(4096, 16);
    expect(ticks[0]).toMatchObject({ value: 16, tall: true });
    expect(ticks[ticks.length - 1]).toMatchObject({ value: 4096, tall: true });
  });

  it("does not produce ticks when max equals step", () => {
    const ticks = computeTicks(0.25, 0.25);
    expect(ticks).toHaveLength(1);
    expect(ticks[0]).toMatchObject({ value: 0.25, tall: true });
  });

  it("includes labels on ticks", () => {
    const ticks = computeTicks(4, 0.25);
    expect(ticks[0].label).toBe("0.25");
    expect(ticks[ticks.length - 1].label).toBe("4");
  });

  it("formats large memory values with k suffix", () => {
    const ticks = computeTicks(4096, 16);
    expect(ticks[ticks.length - 1].label).toBe("4k");
  });
});
