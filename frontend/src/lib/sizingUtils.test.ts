import {
  formatSuggestion,
  highestSeverity,
  recommendationKey,
  recommendationLink,
} from "./sizingUtils";
import type { Recommendation } from "@/api/types";
import { describe, it, expect } from "vitest";

function hint(overrides: Partial<Recommendation>): Recommendation {
  return {
    targetId: "svc1",
    targetName: "web",
    scope: "service",
    category: "over-provisioned",
    severity: "info",
    message: "",
    resource: "memory",
    current: 0,
    suggested: undefined,
    ...overrides,
  } as Recommendation;
}

describe("formatSuggestion", () => {
  it("returns null when suggested is null", () => {
    expect(formatSuggestion(hint({ suggested: undefined }))).toBeNull();
  });

  it("formats single-replica suggestion", () => {
    expect(formatSuggestion(hint({ category: "single-replica", suggested: 3 }))).toBe(
      "Suggested: 3 replicas",
    );
  });

  it("formats manager-has-workloads", () => {
    expect(formatSuggestion(hint({ category: "manager-has-workloads", suggested: 1 }))).toBe(
      "Suggested: drain manager node",
    );
  });

  it("returns null when resource is missing", () => {
    expect(formatSuggestion(hint({ resource: undefined, suggested: 100 }))).toBeNull();
  });

  it("formats memory over-provisioned as reservation", () => {
    const result = formatSuggestion(
      hint({ category: "over-provisioned", resource: "memory", suggested: 256 * 1024 * 1024 }),
    );
    expect(result).toMatch(/^Suggested: memory reservation/);
  });

  it("formats CPU at-limit as limit", () => {
    const result = formatSuggestion(
      hint({ category: "at-limit", resource: "cpu", suggested: 2e9 }),
    );
    expect(result).toMatch(/^Suggested: CPU limit/);
  });
});

describe("highestSeverity", () => {
  it("returns info for empty array", () => {
    expect(highestSeverity([])).toBe("info");
  });

  it("returns critical when present", () => {
    expect(highestSeverity([hint({ severity: "info" }), hint({ severity: "critical" })])).toBe(
      "critical",
    );
  });

  it("returns warning when no critical", () => {
    expect(highestSeverity([hint({ severity: "info" }), hint({ severity: "warning" })])).toBe(
      "warning",
    );
  });
});

describe("recommendationKey", () => {
  it("builds composite key", () => {
    expect(
      recommendationKey(hint({ targetId: "abc", category: "at-limit", resource: "cpu" })),
    ).toBe("abc:at-limit:cpu");
  });
});

describe("recommendationLink", () => {
  it("returns service path for service scope", () => {
    expect(recommendationLink(hint({ scope: "service", targetId: "svc1" }))).toBe("/services/svc1");
  });

  it("returns node path for node scope", () => {
    expect(recommendationLink(hint({ scope: "node", targetId: "node1" }))).toBe("/nodes/node1");
  });

  it("returns null for cluster scope", () => {
    expect(recommendationLink(hint({ scope: "cluster" as Recommendation["scope"] }))).toBeNull();
  });
});
