import { describe, it, expect } from "vitest";
import { computeLayout } from "./layoutDagre";
import type { Node, Edge } from "@xyflow/react";

describe("computeLayout", () => {
  it("assigns positions to nodes", () => {
    const nodes: Node[] = [
      { id: "a", position: { x: 0, y: 0 }, data: {} },
      { id: "b", position: { x: 0, y: 0 }, data: {} },
    ];
    const edges: Edge[] = [{ id: "a-b", source: "a", target: "b" }];
    const result = computeLayout(nodes, edges);

    expect(result.length).toBe(2);
    const posA = result.find((n) => n.id === "a")!.position;
    const posB = result.find((n) => n.id === "b")!.position;
    expect(posA.x).toBeLessThan(posB.x);
  });

  it("handles group nodes with children", () => {
    const nodes: Node[] = [
      { id: "group", position: { x: 0, y: 0 }, data: { label: "Stack" }, type: "group" },
      { id: "child", position: { x: 0, y: 0 }, data: {}, parentId: "group" },
    ];
    const result = computeLayout(nodes, []);
    expect(result.length).toBe(2);
    for (const n of result) {
      expect(typeof n.position.x).toBe("number");
      expect(typeof n.position.y).toBe("number");
    }
  });
});
