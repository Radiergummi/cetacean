import { backoffDelay } from "./backoff";
import { describe, expect, it } from "vitest";

const options = { base: 1000, max: 30_000 };

describe("backoffDelay", () => {
  it("waits the base delay before the first retry", () => {
    expect(backoffDelay(1, options)).toBe(1000);
  });

  it("doubles on each subsequent attempt", () => {
    expect([2, 3, 4, 5].map((attempt) => backoffDelay(attempt, options))).toEqual([
      2000, 4000, 8000, 16_000,
    ]);
  });

  it("never exceeds the ceiling", () => {
    expect(backoffDelay(20, options)).toBe(30_000);
  });

  it("treats a first attempt below one as the first attempt", () => {
    expect(backoffDelay(0, options)).toBe(1000);
  });

  it("keeps each caller's own policy values", () => {
    expect(backoffDelay(1, { base: 5000, max: 30_000 })).toBe(5000);
  });
});
