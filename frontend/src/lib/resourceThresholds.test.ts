import { cpuThresholds, memoryThresholds } from "./resourceThresholds";
import type { Service } from "@/api/types";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/lib/chartColors", () => ({
  getSemanticChartColor: (key: string) => `mock-${key}`,
}));

function makeService(overrides?: {
  reservations?: { NanoCPUs?: number; MemoryBytes?: number };
  limits?: { NanoCPUs?: number; MemoryBytes?: number };
}): Service {
  return {
    Spec: {
      TaskTemplate: {
        Resources: overrides
          ? {
              Reservations: overrides.reservations
                ? {
                    NanoCPUs: overrides.reservations.NanoCPUs ?? 0,
                    MemoryBytes: overrides.reservations.MemoryBytes ?? 0,
                  }
                : undefined,
              Limits: overrides.limits
                ? {
                    NanoCPUs: overrides.limits.NanoCPUs ?? 0,
                    MemoryBytes: overrides.limits.MemoryBytes ?? 0,
                  }
                : undefined,
            }
          : undefined,
      },
    },
  } as Service;
}

describe("cpuThresholds", () => {
  it("returns empty array when no resources defined", () => {
    expect(cpuThresholds(makeService())).toEqual([]);
  });

  it("returns only reservation threshold", () => {
    const result = cpuThresholds(makeService({ reservations: { NanoCPUs: 1e9 } }));

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      label: "Reserved",
      value: 100,
      color: "mock-reserved",
      dash: [12, 6],
    });
  });

  it("returns only limit threshold", () => {
    const result = cpuThresholds(makeService({ limits: { NanoCPUs: 2e9 } }));

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      label: "Limit",
      value: 200,
      color: "mock-critical",
      dash: [12, 6],
    });
  });

  it("returns both thresholds", () => {
    const result = cpuThresholds(
      makeService({ reservations: { NanoCPUs: 5e8 }, limits: { NanoCPUs: 1e9 } }),
    );

    expect(result).toHaveLength(2);
    expect(result[0].label).toBe("Reserved");
    expect(result[0].value).toBe(50);
    expect(result[1].label).toBe("Limit");
    expect(result[1].value).toBe(100);
  });

  it("converts NanoCPUs correctly (1e9 nano = 100%)", () => {
    const result = cpuThresholds(makeService({ reservations: { NanoCPUs: 2.5e8 } }));

    expect(result[0].value).toBe(25);
  });
});

describe("memoryThresholds", () => {
  it("returns empty array when no resources defined", () => {
    expect(memoryThresholds(makeService())).toEqual([]);
  });

  it("returns reservation threshold in bytes", () => {
    const result = memoryThresholds(makeService({ reservations: { MemoryBytes: 536870912 } }));

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      label: "Reserved",
      value: 536870912,
      color: "mock-reserved",
      dash: [12, 6],
    });
  });

  it("returns limit threshold in bytes", () => {
    const result = memoryThresholds(makeService({ limits: { MemoryBytes: 1073741824 } }));

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      label: "Limit",
      value: 1073741824,
      color: "mock-critical",
      dash: [12, 6],
    });
  });

  it("returns both thresholds", () => {
    const result = memoryThresholds(
      makeService({ reservations: { MemoryBytes: 256e6 }, limits: { MemoryBytes: 512e6 } }),
    );

    expect(result).toHaveLength(2);
    expect(result[0].label).toBe("Reserved");
    expect(result[1].label).toBe("Limit");
  });
});
