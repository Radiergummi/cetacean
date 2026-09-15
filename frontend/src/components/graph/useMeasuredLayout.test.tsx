import { useMeasuredLayout, type Graph, type Layout } from "./useMeasuredLayout";
import { renderHook, waitFor } from "@testing-library/react";
import { ReactFlowProvider } from "@xyflow/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

// jsdom measures nothing, so React Flow never reports the sizes a layout waits
// for. Only that signal is stood in for; the hook's own state machine is real.
vi.mock("@xyflow/react", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@xyflow/react")>()),
  useNodesInitialized: () => true,
}));

const graph: Graph = {
  nodes: [{ id: "a", position: { x: 0, y: 0 }, data: {} }],
  edges: [],
};

const wrapper = ({ children }: { children: ReactNode }) => (
  <ReactFlowProvider>{children}</ReactFlowProvider>
);

describe("useMeasuredLayout", () => {
  it("reports a failure when the graph cannot be laid out", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const failing: Layout = () => Promise.reject(new Error("no engine"));

    const { result } = renderHook(() => useMeasuredLayout(graph, failing), { wrapper });

    await waitFor(() => expect(result.current.failed).toBe(true));
    expect(result.current.bounds).toBeNull();

    warn.mockRestore();
  });

  it("reports no failure once the graph is placed", async () => {
    const placing: Layout = (nodes, edges) =>
      Promise.resolve({ nodes, edges, bounds: { width: 10, height: 10 } });

    const { result } = renderHook(() => useMeasuredLayout(graph, placing), { wrapper });

    await waitFor(() => expect(result.current.bounds).toEqual({ width: 10, height: 10 }));
    expect(result.current.failed).toBe(false);
  });
});
