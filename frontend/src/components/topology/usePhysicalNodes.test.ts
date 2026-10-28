import { usePhysicalNodes } from "./usePhysicalNodes";
import { act, renderHook } from "@testing-library/react";
import type { Node } from "@xyflow/react";
import { describe, expect, it } from "vitest";

function card(id: string, label: string): Node {
  return { id, position: { x: 0, y: 0 }, data: { label } };
}

function dragTo(onNodesChange: ReturnType<typeof usePhysicalNodes>["onNodesChange"]) {
  act(() => {
    onNodesChange([{ type: "position", id: "a", position: { x: 300, y: 120 } }]);
  });
}

describe("usePhysicalNodes", () => {
  it("keeps a dragged card where it was dropped", () => {
    const { result } = renderHook(({ nodes }) => usePhysicalNodes(nodes), {
      initialProps: { nodes: [card("a", "one")] },
    });

    dragTo(result.current.onNodesChange);

    expect(result.current.nodes[0]?.position).toEqual({ x: 300, y: 120 });
  });

  it("keeps the position across a refresh of the same cards, taking the new data", () => {
    const { result, rerender } = renderHook(({ nodes }) => usePhysicalNodes(nodes), {
      initialProps: { nodes: [card("a", "one")] },
    });

    dragTo(result.current.onNodesChange);
    rerender({ nodes: [card("a", "two")] });

    expect(result.current.nodes[0]?.position).toEqual({ x: 300, y: 120 });
    expect(result.current.nodes[0]?.data).toEqual({ label: "two" });
  });

  it("starts over from the computed grid when the set of cards changes", () => {
    const { result, rerender } = renderHook(({ nodes }) => usePhysicalNodes(nodes), {
      initialProps: { nodes: [card("a", "one")] },
    });

    dragTo(result.current.onNodesChange);
    rerender({ nodes: [card("a", "one"), card("b", "two")] });

    expect(result.current.nodes.map(({ id, position }) => ({ id, position }))).toEqual([
      { id: "a", position: { x: 0, y: 0 } },
      { id: "b", position: { x: 0, y: 0 } },
    ]);
  });
});
