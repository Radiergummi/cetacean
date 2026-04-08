import { flattenSearchResults, resourcePath, splitStackPrefix } from "./searchConstants";
import { describe, expect, it } from "vitest";

describe("flattenSearchResults", () => {
  it("returns empty array for empty results", () => {
    expect(flattenSearchResults({ results: {} })).toEqual([]);
  });

  it("orders results by typeOrder (services before nodes before tasks)", () => {
    const response = {
      results: {
        tasks: [{ id: "t1", name: "task1", detail: "" }],
        nodes: [{ id: "n1", name: "node1", detail: "" }],
        services: [{ id: "s1", name: "svc1", detail: "" }],
      },
    };
    const flat = flattenSearchResults(response);

    expect(flat.map(({ type }) => type)).toEqual(["services", "nodes", "tasks"]);
    expect(flat[0].result.id).toBe("s1");
    expect(flat[1].result.id).toBe("n1");
    expect(flat[2].result.id).toBe("t1");
  });

  it("restricts to single type when filterType is provided", () => {
    const response = {
      results: {
        services: [{ id: "s1", name: "svc1", detail: "" }],
        nodes: [{ id: "n1", name: "node1", detail: "" }],
      },
    };
    const flat = flattenSearchResults(response, "nodes");

    expect(flat).toHaveLength(1);
    expect(flat[0].type).toBe("nodes");
    expect(flat[0].result.id).toBe("n1");
  });

  it("skips missing types gracefully", () => {
    const response = {
      results: {
        services: [{ id: "s1", name: "svc1", detail: "" }],
      },
    };
    const flat = flattenSearchResults(response);

    expect(flat).toHaveLength(1);
    expect(flat[0].type).toBe("services");
  });
});

describe("resourcePath", () => {
  it("returns correct path for plural types", () => {
    expect(resourcePath("nodes", "abc")).toBe("/nodes/abc");
    expect(resourcePath("services", "def")).toBe("/services/def");
    expect(resourcePath("volumes", "vol-id", "my-vol")).toBe("/volumes/my-vol");
    expect(resourcePath("stacks", "stack-id", "my-stack")).toBe("/stacks/my-stack");
  });

  it("returns correct path for singular types", () => {
    expect(resourcePath("node", "abc")).toBe("/nodes/abc");
    expect(resourcePath("service", "def")).toBe("/services/def");
    expect(resourcePath("volume", "vol-id", "my-vol")).toBe("/volumes/my-vol");
    expect(resourcePath("stack", "stack-id", "my-stack")).toBe("/stacks/my-stack");
  });

  it("returns null for unknown type", () => {
    expect(resourcePath("unknown", "abc")).toBeNull();
  });
});

describe("splitStackPrefix", () => {
  it("splits on first underscore", () => {
    expect(splitStackPrefix("mystack_web")).toEqual({ prefix: "mystack", name: "web" });
  });

  it("returns null prefix when no underscore", () => {
    expect(splitStackPrefix("standalone")).toEqual({ prefix: null, name: "standalone" });
  });
});
