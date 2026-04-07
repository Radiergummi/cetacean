import { feedForPath } from "./AtomFeedLink";
import { describe, it, expect } from "vitest";

describe("feedForPath", () => {
  it("returns null for unsupported paths", () => {
    expect(feedForPath("/cluster", "")).toBeNull();
    expect(feedForPath("/topology", "")).toBeNull();
    expect(feedForPath("/", "")).toBeNull();
    expect(feedForPath("/debug/pprof", "")).toBeNull();
  });

  it("returns feed for resource list routes", () => {
    const resources = [
      "nodes",
      "services",
      "tasks",
      "stacks",
      "configs",
      "secrets",
      "networks",
      "volumes",
    ];

    for (const resource of resources) {
      const result = feedForPath(`/${resource}`, "");
      expect(result).not.toBeNull();
      expect(result!.href).toBe(`/${resource}`);
      expect(result!.title).toBe(resource.charAt(0).toUpperCase() + resource.slice(1));
    }
  });

  it("returns feed for resource detail routes", () => {
    const result = feedForPath("/services/abc123", "");
    expect(result).not.toBeNull();
    expect(result!.href).toBe("/services/abc123");
    expect(result!.title).toBe("Service abc123");
  });

  it("returns feed for standalone routes", () => {
    const history = feedForPath("/history", "");
    expect(history).not.toBeNull();
    expect(history!.title).toBe("History");

    const recommendations = feedForPath("/recommendations", "");
    expect(recommendations).not.toBeNull();
    expect(recommendations!.title).toBe("Recommendations");
  });

  it("returns feed for search with query string", () => {
    const result = feedForPath("/search", "?q=myservice");
    expect(result).not.toBeNull();
    expect(result!.href).toBe("/search?q=myservice");
    expect(result!.title).toBe("Search Results");
  });

  it("returns feed for search without query string", () => {
    const result = feedForPath("/search", "");
    expect(result).not.toBeNull();
    expect(result!.href).toBe("/search");
  });
});
