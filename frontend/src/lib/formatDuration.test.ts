import { formatDuration } from "./format";
import { afterEach, describe, expect, it } from "vitest";

const original = Intl.DurationFormat;

afterEach(() => {
  Object.defineProperty(Intl, "DurationFormat", {
    value: original,
    configurable: true,
    writable: true,
  });
});

// Firefox before 136 and older Safari lack Intl.DurationFormat; a precise
// duration there crashed the whole service page.
describe("formatDuration without Intl.DurationFormat", () => {
  it("still formats a precise duration", () => {
    Object.defineProperty(Intl, "DurationFormat", {
      value: undefined,
      configurable: true,
      writable: true,
    });

    expect(formatDuration(3_725 * 1e9, true)).toBe("1h 2m 5s");
  });
});
