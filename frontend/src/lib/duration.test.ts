import { bestDurationUnit } from "./duration";
import { describe, it, expect } from "vitest";

describe("bestDurationUnit", () => {
  it("returns seconds for sub-minute values", () => {
    expect(bestDurationUnit(5_000_000_000).label).toBe("seconds");
  });

  it("returns minutes for exact minute multiples", () => {
    expect(bestDurationUnit(120_000_000_000).label).toBe("minutes");
  });

  it("returns hours for exact hour multiples", () => {
    expect(bestDurationUnit(7_200_000_000_000).label).toBe("hours");
  });

  it("returns days for exact day multiples", () => {
    expect(bestDurationUnit(172_800_000_000_000).label).toBe("days");
  });

  it("returns seconds when not evenly divisible by minutes", () => {
    // 90 seconds = 1.5 minutes, not evenly divisible
    expect(bestDurationUnit(90_000_000_000).label).toBe("seconds");
  });

  it("returns minutes when evenly divisible by minutes but not hours", () => {
    // 30 minutes
    expect(bestDurationUnit(1_800_000_000_000).label).toBe("minutes");
  });

  it("returns seconds for zero", () => {
    expect(bestDurationUnit(0).label).toBe("seconds");
  });

  it("returns seconds for values smaller than 1 second", () => {
    expect(bestDurationUnit(500_000_000).label).toBe("seconds");
  });
});
