import { toFlow } from "./layout";
import { describe, expect, it } from "vitest";

describe("toFlow", () => {
  it("maps every graph edge to a React Flow edge", () => {
    const { edges } = toFlow({
      view: "network",
      nodes: [
        { id: "svc-a", label: "api", type: "service" },
        { id: "net-1", label: "backend", type: "network" },
      ],
      edges: [{ source: "svc-a", target: "net-1", label: "api" }],
    });

    expect(edges).toEqual([
      { id: "svc-a--net-1", source: "svc-a", target: "net-1", label: "api", animated: false },
    ]);
  });

  it("puts each node kind of a view in its own column", () => {
    const { nodes } = toFlow({
      view: "placement",
      nodes: [
        { id: "node-1", label: "worker-a", type: "node" },
        { id: "svc-a", label: "api", type: "service" },
      ],
      edges: [],
    });

    expect(nodes[0]?.position.x).toBe(0);
    expect(nodes[1]?.position.x).toBeGreaterThan(0);
  });

  it("stacks the members of one column without overlapping them", () => {
    const { nodes } = toFlow({
      view: "network",
      nodes: [
        { id: "a", label: "a", type: "service" },
        { id: "b", label: "b", type: "service" },
      ],
      edges: [],
    });

    expect(nodes[0]?.position.y).not.toBe(nodes[1]?.position.y);
  });

  it("centres the shorter column against the taller one", () => {
    const { nodes } = toFlow({
      view: "network",
      nodes: [
        { id: "a", label: "a", type: "service" },
        { id: "b", label: "b", type: "service" },
        { id: "c", label: "c", type: "service" },
        { id: "net", label: "backend", type: "network" },
      ],
      edges: [],
    });

    const network = nodes.find(({ id }) => id === "net");

    expect(network?.position.y).toBe(84);
  });

  it("keeps a node kind the view does not place on the canvas", () => {
    // The server and the widget ship separately, so a vertex kind added on one
    // side has to stay visible on the other rather than land off-canvas.
    const { nodes } = toFlow({
      view: "network",
      nodes: [{ id: "x", label: "volume", type: "volume" }],
      edges: [],
    });

    expect(nodes[0]?.position.x).toBe(0);
  });
});
