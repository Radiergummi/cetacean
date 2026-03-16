import Sparkline from "./Sparkline";
import { render } from "@testing-library/react";
import { describe, it, expect } from "vitest";

describe("Sparkline", () => {
  it("renders nothing for insufficient data", () => {
    const { container } = render(<Sparkline data={[5]} />);
    expect(container.querySelector("svg")).toBeNull();
  });

  it("renders an svg with a polyline", () => {
    const { container } = render(<Sparkline data={[10, 20, 30]} />);
    const svg = container.querySelector("svg");
    expect(svg).toBeInTheDocument();
    const polyline = container.querySelector("polyline");
    expect(polyline).toBeInTheDocument();
    expect(polyline?.getAttribute("points")).toBeTruthy();
  });

  it("uses custom dimensions", () => {
    const { container } = render(
      <Sparkline
        data={[1, 2]}
        width={100}
        height={32}
      />,
    );
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("width")).toBe("100");
    expect(svg?.getAttribute("height")).toBe("32");
  });
});
