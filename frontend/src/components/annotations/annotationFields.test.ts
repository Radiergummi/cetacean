import { annotationBody, fromLocalInput, parseTag, toLocalInput } from "./annotationFields";
import { describe, expect, it } from "vitest";

describe("parseTag", () => {
  it("keeps a tag the API takes, lowercased", () => {
    expect(parseTag(" Deploy ")).toBe("deploy");
    expect(parseTag("load-test.v2_a")).toBe("load-test.v2_a");
  });

  it("refuses one it does not", () => {
    expect(parseTag("a b")).toBeNull();
    expect(parseTag("")).toBeNull();
    expect(parseTag("x".repeat(41))).toBeNull();
    expect(parseTag("Bad!")).toBeNull();
  });
});

describe("local times", () => {
  it("round-trips a whole minute to the same instant in UTC", () => {
    const at = Date.UTC(2026, 10, 2, 13, 37) / 1000;

    expect(toLocalInput(at)).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
    expect(fromLocalInput(toLocalInput(at))).toBe(new Date(at * 1000).toISOString());
  });

  it("drops the seconds of a time it shows", () => {
    const at = Date.UTC(2026, 10, 2, 13, 37, 42) / 1000;

    expect(fromLocalInput(toLocalInput(at))).toBe(new Date((at - 42) * 1000).toISOString());
  });
});

describe("annotationBody", () => {
  const from = toLocalInput(Date.UTC(2026, 10, 2, 13, 0) / 1000);
  const to = toLocalInput(Date.UTC(2026, 10, 2, 14, 0) / 1000);
  const service = { type: "service", id: "svc1", name: "web" } as const;

  it("names the resource it is about", () => {
    expect(annotationBody({ text: "x", tags: ["a"], from, to, scope: service })).toEqual({
      text: "x",
      tags: ["a"],
      timestamp: "2026-11-02T13:00:00.000Z",
      until: "2026-11-02T14:00:00.000Z",
      resources: [{ type: "service", id: "svc1" }],
    });
  });

  it("names nothing when cluster-wide, and has no end without one", () => {
    expect(annotationBody({ text: "x", tags: [], from, to: "", scope: "cluster" })).toEqual({
      text: "x",
      tags: [],
      timestamp: "2026-11-02T13:00:00.000Z",
    });
  });
});
