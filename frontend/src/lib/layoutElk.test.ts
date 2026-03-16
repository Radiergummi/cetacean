import { computeLayout } from "./layoutElk";
import type { Node, Edge } from "@xyflow/react";
import { describe, it, expect } from "vitest";

describe("computeLayout (ELK)", () => {
  it("assigns positions to nodes in LR order", async () => {
    const nodes: Node[] = [
      { id: "a", position: { x: 0, y: 0 }, data: {} },
      { id: "b", position: { x: 0, y: 0 }, data: {} },
    ];
    const edges: Edge[] = [{ id: "a-b", source: "a", target: "b" }];
    const result = await computeLayout(nodes, edges);

    expect(result.nodes.length).toBe(2);
    const posA = result.nodes.find((n) => n.id === "a")!.position;
    const posB = result.nodes.find((n) => n.id === "b")!.position;
    expect(posA.x).toBeLessThan(posB.x);
  });

  it("group nodes get explicit width/height style", async () => {
    const nodes: Node[] = [
      { id: "group", position: { x: 0, y: 0 }, data: { label: "Stack" }, type: "stackGroup" },
      { id: "child", position: { x: 0, y: 0 }, data: {}, parentId: "group" },
    ];
    const result = await computeLayout(nodes, []);
    const group = result.nodes.find((n) => n.id === "group")!;
    const child = result.nodes.find((n) => n.id === "child")!;

    expect(group.style).toBeDefined();
    expect((group.style as any).width).toBeGreaterThan(0);
    expect((group.style as any).height).toBeGreaterThan(0);

    // Child position should be relative to parent (non-negative)
    expect(child.position.x).toBeGreaterThanOrEqual(0);
    expect(child.position.y).toBeGreaterThanOrEqual(0);
  });

  it("returns bend points on edges", async () => {
    const nodes: Node[] = [
      { id: "a", position: { x: 0, y: 0 }, data: {} },
      { id: "b", position: { x: 0, y: 0 }, data: {} },
    ];
    const edges: Edge[] = [{ id: "a-b", source: "a", target: "b" }];
    const result = await computeLayout(nodes, edges);

    expect(result.edges.length).toBe(1);
    expect(result.edges[0].data!.bendPoints).toBeDefined();
    expect((result.edges[0].data as any).bendPoints.length).toBeGreaterThanOrEqual(2);
  });
});
