import { MeasuredGraph } from "./MeasuredGraph";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// jsdom measures nothing, so React Flow never reports the sizes a layout waits
// for; without this the layout below is never reached at all.
vi.mock("@xyflow/react", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@xyflow/react")>()),
  useNodesInitialized: () => true,
}));

vi.mock("@/lib/graphLayout", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/graphLayout")>()),
  layoutGraph: () => Promise.reject(new Error("no engine")),
}));

describe("MeasuredGraph", () => {
  it("says the graph could not be drawn rather than showing an empty frame", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});

    render(
      <MeasuredGraph
        graph={{ nodes: [{ id: "a", position: { x: 0, y: 0 }, data: {} }], edges: [] }}
        nodeTypes={{}}
        label="Topology of stack shop"
      />,
    );

    expect(await screen.findByRole("status")).toHaveTextContent(/could not be drawn/i);
  });
});
