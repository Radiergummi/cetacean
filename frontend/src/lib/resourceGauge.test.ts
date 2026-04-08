import { cpuGaugePercent, memoryGaugePercent } from "./resourceGauge";
import { describe, it, expect } from "vitest";

describe("cpuGaugePercent", () => {
  it("returns null when usage is null", () => {
    expect(cpuGaugePercent(null, 1e9)).toBeNull();
  });

  it("returns null when limit is undefined", () => {
    expect(cpuGaugePercent(50, undefined)).toBeNull();
  });

  it("returns null when limit is zero", () => {
    expect(cpuGaugePercent(50, 0)).toBeNull();
  });

  it("computes ratio when usage matches limit", () => {
    // 1 core limit = 1e9 nano, usage = 100% of 1 vCPU
    // 100 / (1e9 / 1e7) = 100 / 100 = 1
    expect(cpuGaugePercent(100, 1e9)).toBe(1);
  });

  it("computes half ratio when using half the limit", () => {
    expect(cpuGaugePercent(50, 1e9)).toBe(0.5);
  });

  it("computes correctly for fractional core limits", () => {
    // 0.25 core limit = 2.5e8 nano, usage = 25% of 1 vCPU
    // 25 / (2.5e8 / 1e7) = 25 / 25 = 1
    expect(cpuGaugePercent(25, 2.5e8)).toBe(1);
  });

  it("can exceed 1 when over limit", () => {
    expect(cpuGaugePercent(200, 1e9)).toBe(2);
  });
});

describe("memoryGaugePercent", () => {
  it("returns null when usage is null", () => {
    expect(memoryGaugePercent(null, 1024)).toBeNull();
  });

  it("returns null when limit is undefined", () => {
    expect(memoryGaugePercent(512, undefined)).toBeNull();
  });

  it("returns null when limit is zero", () => {
    expect(memoryGaugePercent(512, 0)).toBeNull();
  });

  it("computes 50% when using half the limit", () => {
    expect(memoryGaugePercent(512, 1024)).toBe(50);
  });

  it("computes 100% at limit", () => {
    expect(memoryGaugePercent(1024, 1024)).toBe(100);
  });

  it("can exceed 100% when over limit", () => {
    expect(memoryGaugePercent(2048, 1024)).toBe(200);
  });
});
