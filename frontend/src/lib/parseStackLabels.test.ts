import { parseStackLabels, stripStackPrefix, stackNamespaceLabel } from "./parseStackLabels";
import { describe, it, expect } from "vitest";

describe("parseStackLabels", () => {
  it("separates the stack from the labels worth showing", () => {
    const { entries, stack } = parseStackLabels({
      [stackNamespaceLabel]: "web",
      "com.example.team": "platform",
    });

    expect(stack).toBe("web");
    expect(entries).toEqual([["com.example.team", "platform"]]);
  });

  it("handles a resource with no labels at all", () => {
    expect(parseStackLabels(null)).toEqual({ entries: [], stack: undefined });
    expect(parseStackLabels(undefined)).toEqual({ entries: [], stack: undefined });
    expect(parseStackLabels({})).toEqual({ entries: [], stack: undefined });
  });
});

describe("stripStackPrefix", () => {
  it("drops the stack prefix Docker adds", () => {
    expect(stripStackPrefix("web_api", "web")).toBe("api");
  });

  it("leaves a name that only joined the stack by label", () => {
    expect(stripStackPrefix("shared-cache", "web")).toBe("shared-cache");
  });

  it("never guesses at an underscore when there is no stack", () => {
    expect(stripStackPrefix("my_service")).toBe("my_service");
    expect(stripStackPrefix("my_service", undefined)).toBe("my_service");
  });

  it("only strips its own stack's prefix", () => {
    expect(stripStackPrefix("web_api", "db")).toBe("web_api");
  });
});
