import { pointerTo } from "./jsonPointer";
import { describe, expect, it } from "vitest";

describe("pointerTo", () => {
  it("escapes the two characters a pointer reserves", () => {
    expect(pointerTo("plain")).toBe("/plain");
    expect(pointerTo("app.kubernetes.io/name")).toBe("/app.kubernetes.io~1name");
    expect(pointerTo("m~n")).toBe("/m~0n");
    expect(pointerTo("~1")).toBe("/~01");
  });
});
