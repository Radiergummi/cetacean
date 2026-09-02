import type { TopologyGraphData, TopologyView } from "./layout";
import { TopologyGraph } from "./TopologyGraph";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

/**
 * A graph exactly as get_topology returns it. The widget is built and shipped
 * separately from the server, so nothing type-checks this shape against the Go
 * struct it mirrors — spelling it out here is the contract.
 */
const networkGraph: TopologyGraphData = {
  view: "network",
  nodes: [
    { id: "svc-a", label: "api", type: "service", group: "shop", detail: "api:1" },
    { id: "svc-b", label: "worker", type: "service", group: "shop", detail: "worker:1" },
    { id: "net-1", label: "backend", type: "network", detail: "overlay" },
  ],
  edges: [
    { source: "svc-a", target: "net-1", label: "api" },
    { source: "svc-b", target: "net-1" },
  ],
};

describe("TopologyGraph", () => {
  it("renders a vertex per graph node", () => {
    render(
      <TopologyGraph
        graph={networkGraph}
        onViewChange={vi.fn<(view: TopologyView) => void>()}
      />,
    );

    expect(screen.getByText("api")).toBeInTheDocument();
    expect(screen.getByText("worker")).toBeInTheDocument();
    expect(screen.getByText("backend")).toBeInTheDocument();
  });

  // React Flow draws an edge only once both endpoints have been measured, which
  // needs a real layout pass jsdom does not perform — so the graph-to-canvas
  // edge mapping is asserted in layout.test.ts, where it is genuinely visible.
  it("lays the two node kinds out in separate columns", () => {
    const { container } = render(
      <TopologyGraph
        graph={networkGraph}
        onViewChange={vi.fn<(view: TopologyView) => void>()}
      />,
    );

    const columnOf = (id: string) => {
      const element = container.querySelector(`.react-flow__node[data-id="${id}"]`);

      return element?.getAttribute("style")?.match(/translate\((-?\d+)px/)?.[1];
    };

    expect(columnOf("svc-a")).toBe(columnOf("svc-b"));
    expect(columnOf("net-1")).not.toBe(columnOf("svc-a"));
  });

  it("asks for the other view when the switcher is used", () => {
    const onViewChange = vi.fn<(view: TopologyView) => void>();

    render(
      <TopologyGraph
        graph={networkGraph}
        onViewChange={onViewChange}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /placement/i }));

    expect(onViewChange).toHaveBeenCalledWith("placement");
  });

  it("does not ask for the view it is already showing", () => {
    const onViewChange = vi.fn<(view: TopologyView) => void>();

    render(
      <TopologyGraph
        graph={networkGraph}
        onViewChange={onViewChange}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: /network/i }));

    expect(onViewChange).not.toHaveBeenCalled();
  });

  it("says a view is empty rather than showing a blank canvas", () => {
    render(
      <TopologyGraph
        graph={{ view: "placement", nodes: [], edges: [] }}
        onViewChange={vi.fn<(view: TopologyView) => void>()}
      />,
    );

    expect(screen.getByText(/nothing to show/i)).toBeInTheDocument();

    // The switcher has to survive an empty result, or a caller who lands on the
    // empty view cannot get back to the populated one.
    expect(screen.getByRole("button", { name: /network/i })).toBeInTheDocument();
  });
});
