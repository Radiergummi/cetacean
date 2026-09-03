import { backoffDelay, retryAfterDelay } from "./backoff";
import { afterEach, describe, expect, it, vi } from "vitest";

const options = { base: 1000, max: 30_000 };

/**
 * Pins Math.random so a jittered delay is exact rather than merely in range.
 * At 0.5 the backoff spread is exactly 1, which is what lets the nominal
 * schedule below still be asserted on round numbers.
 */
function randomReturns(value: number) {
  vi.spyOn(Math, "random").mockReturnValue(value);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("backoffDelay", () => {
  it("waits the base delay before the first retry", () => {
    randomReturns(0.5);

    expect(backoffDelay(1, options)).toBe(1000);
  });

  it("doubles on each subsequent attempt", () => {
    randomReturns(0.5);

    expect([2, 3, 4, 5].map((attempt) => backoffDelay(attempt, options))).toEqual([
      2000, 4000, 8000, 16_000,
    ]);
  });

  it("stops doubling at the ceiling", () => {
    randomReturns(0.5);

    expect(backoffDelay(20, options)).toBe(30_000);
  });

  it("treats a first attempt below one as the first attempt", () => {
    randomReturns(0.5);

    expect(backoffDelay(0, options)).toBe(1000);
  });

  it("keeps each caller's own policy values", () => {
    randomReturns(0.5);

    expect(backoffDelay(1, { base: 5000, max: 30_000 })).toBe(5000);
  });

  it("spreads the delay a quarter below nominal at the low end", () => {
    randomReturns(0);

    expect(backoffDelay(1, options)).toBe(750);
  });

  it("spreads the delay a quarter above nominal at the high end", () => {
    randomReturns(1);

    expect(backoffDelay(1, options)).toBe(1250);
  });

  it("does not hand two clients failing together the same delay", () => {
    // A deterministic schedule is what turns one restart into a series of
    // synchronized spikes, so the spread is the behaviour under test.
    const delays = new Set(Array.from({ length: 50 }, () => backoffDelay(3, options)));

    expect(delays.size).toBeGreaterThan(1);

    for (const delay of delays) {
      expect(delay).toBeGreaterThanOrEqual(3000);
      expect(delay).toBeLessThanOrEqual(5000);
    }
  });
});

describe("retryAfterDelay", () => {
  it("never waits less than the server asked for", () => {
    // Waiting less than Retry-After is the one thing a client must not do, so
    // the spread is strictly later and the server's value is the lower bound.
    randomReturns(0);

    expect(retryAfterDelay(5000)).toBe(5000);
  });

  it("stretches by up to half again", () => {
    randomReturns(1);

    expect(retryAfterDelay(5000)).toBe(7500);
  });

  it("scatters clients rejected at the same instant", () => {
    const delays = new Set(Array.from({ length: 50 }, () => retryAfterDelay(5000)));

    expect(delays.size).toBeGreaterThan(1);

    for (const delay of delays) {
      expect(delay).toBeGreaterThanOrEqual(5000);
      expect(delay).toBeLessThanOrEqual(7500);
    }
  });
});
