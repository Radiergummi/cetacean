import { computeLayout } from "./layoutElk";
import type { Node, Edge } from "@xyflow/react";
import { describe, it, expect, vi } from "vitest";

describe("loadElk", () => {
  it("retries after a failed load rather than caching the failure", async () => {
    let attempts = 0;

    vi.resetModules();
    vi.doMock("elkjs/lib/elk.bundled.js", () => ({
      default: class {
        constructor() {
          attempts += 1;

          if (attempts === 1) {
            throw new Error("chunk gone");
          }
        }
      },
    }));

    try {
      const { loadElk } = await import("./layoutElk");

      await expect(loadElk()).rejects.toThrow("chunk gone");
      await expect(loadElk()).resolves.toBeDefined();
      expect(attempts).toBe(2);
    } finally {
      vi.doUnmock("elkjs/lib/elk.bundled.js");
      vi.resetModules();
    }
  });
});

describe("computeLayout (ELK)", () => {
  it("assigns positions to nodes in LR order", async () => {
    const nodes: Node[] = [
      { id: "a", position: { x: 0, y: 0 }, data: {} },
      { id: "b", position: { x: 0, y: 0 }, data: {} },
    ];
    const edges: Edge[] = [{ id: "a-b", source: "a", target: "b" }];
    const result = await computeLayout(nodes, edges);

    expect(result.nodes.length).toBe(2);
    const posA = result.nodes.find(({ id }) => id === "a")!.position;
    const posB = result.nodes.find(({ id }) => id === "b")!.position;
    expect(posA.x).toBeLessThan(posB.x);
  });

  it("group nodes get explicit width/height style", async () => {
    const nodes: Node[] = [
      { id: "group", position: { x: 0, y: 0 }, data: { label: "Stack" }, type: "stackGroup" },
      { id: "child", position: { x: 0, y: 0 }, data: {}, parentId: "group" },
    ];
    const result = await computeLayout(nodes, []);
    const group = result.nodes.find(({ id }) => id === "group")!;
    const child = result.nodes.find(({ id }) => id === "child")!;

    expect(group.style).toBeDefined();
    expect((group.style as any).width).toBeGreaterThan(0);
    expect((group.style as any).height).toBeGreaterThan(0);

    // Child position should be relative to parent (non-negative)
    expect(child.position.x).toBeGreaterThanOrEqual(0);
    expect(child.position.y).toBeGreaterThanOrEqual(0);
  });

  it("packs a node at the size React Flow measured it", async () => {
    const nodes: Node[] = [
      { id: "a", position: { x: 0, y: 0 }, data: {}, measured: { width: 300, height: 400 } },
      { id: "b", position: { x: 0, y: 0 }, data: {}, measured: { width: 300, height: 40 } },
    ];
    const edges: Edge[] = [{ id: "a-b", source: "a", target: "b" }];
    const result = await computeLayout(nodes, edges);

    // Neither holds unless the sizes ELK packed are the measured ones.
    expect(result.nodes.find(({ id }) => id === "b")!.position.x).toBeGreaterThanOrEqual(300);
    expect(result.bounds.height).toBeGreaterThanOrEqual(400);
  });

  it("keeps two edges apart when their endpoint names run together", async () => {
    // Node ids carry their kind — "service:web" — so a key joining a pair on
    // ":" cannot tell "a:b"→"c" from "a"→"b:c".
    const nodes: Node[] = ["a:b", "c", "a", "b:c"].map((id) => ({
      id,
      position: { x: 0, y: 0 },
      data: {},
    }));
    const edges: Edge[] = [
      { id: "first", source: "a:b", target: "c" },
      { id: "second", source: "a", target: "b:c" },
    ];

    const result = await computeLayout(nodes, edges);
    const position = (id: string) => result.nodes.find((node) => node.id === id)!.position;

    expect(position("a:b").x).toBeLessThan(position("c").x);
    expect(position("a").x).toBeLessThan(position("b:c").x);
  });

  it("returns bend points on edges", async () => {
    const nodes: Node[] = [
      { id: "a", position: { x: 0, y: 0 }, data: {} },
      { id: "b", position: { x: 0, y: 0 }, data: {} },
    ];
    const edges: Edge[] = [{ id: "a-b", source: "a", target: "b" }];
    const result = await computeLayout(nodes, edges);

    expect(result.edges.length).toBe(1);
    expect(result.edges[0]?.data?.["bendPoints"]).toBeDefined();
    expect((result.edges[0]!.data as any).bendPoints.length).toBeGreaterThanOrEqual(2);
  });
});
