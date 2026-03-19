import { describe, it, expect } from "vitest";
import { getCursorContext, getTokenBounds, segmentPrefixMatch } from "./useQueryCompletion";

describe("getCursorContext", () => {
  it("returns metric context outside braces", () => {
    expect(getCursorContext("up", 2)).toEqual({ type: "metric" });
  });

  it("returns metric context for empty query", () => {
    expect(getCursorContext("", 0)).toEqual({ type: "metric" });
  });

  it("returns metric context after closing brace", () => {
    expect(getCursorContext('up{job="x"} + ra', 16)).toEqual({ type: "metric" });
  });

  it("returns label context inside empty braces", () => {
    expect(getCursorContext("up{", 3)).toEqual({ type: "label", metricName: "up" });
  });

  it("returns label context with partial label name", () => {
    expect(getCursorContext("up{jo", 5)).toEqual({ type: "label", metricName: "up" });
  });

  it("returns label context after comma", () => {
    expect(getCursorContext('up{job="x",ins', 14)).toEqual({ type: "label", metricName: "up" });
  });

  it("returns value context after equals-quote", () => {
    expect(getCursorContext('up{job="pro', 11)).toEqual({
      type: "value",
      metricName: "up",
      labelName: "job",
    });
  });

  it("returns value context with regex match operator", () => {
    expect(getCursorContext('up{job=~"pro', 12)).toEqual({
      type: "value",
      metricName: "up",
      labelName: "job",
    });
  });

  it("returns value context for empty value", () => {
    expect(getCursorContext('up{job="', 8)).toEqual({
      type: "value",
      metricName: "up",
      labelName: "job",
    });
  });

  it("extracts metric name with colons", () => {
    expect(getCursorContext("namespace:container_cpu:sum_rate{", 33)).toEqual({
      type: "label",
      metricName: "namespace:container_cpu:sum_rate",
    });
  });

  it("handles nested braces in subqueries", () => {
    // After the outer } we're in metric context, not label context
    expect(getCursorContext('rate(up{job="x"}[5m]) + ra', 26)).toEqual({ type: "metric" });
  });

  it("returns label context with empty metric name for bare braces", () => {
    expect(getCursorContext("{", 1)).toEqual({ type: "label", metricName: "" });
  });
});

describe("segmentPrefixMatch", () => {
  it("matches full prefix", () => {
    expect(segmentPrefixMatch("go_gc", "go_gc_cleanups_total")).toBe(true);
  });

  it("matches abbreviated segments", () => {
    expect(segmentPrefixMatch("ggclext", "go_gc_cleanups_executed_cleanups_total")).toBe(true);
  });

  it("matches skipping segments", () => {
    expect(segmentPrefixMatch("gotot", "go_gc_cleanups_executed_cleanups_total")).toBe(true);
  });

  it("matches single character per segment", () => {
    expect(segmentPrefixMatch("ggt", "go_gc_total")).toBe(true);
  });

  it("rejects when characters are out of order", () => {
    expect(segmentPrefixMatch("tg", "go_total")).toBe(false);
  });

  it("rejects when query has characters not in any segment", () => {
    expect(segmentPrefixMatch("gx", "go_gc_total")).toBe(false);
  });

  it("matches exact metric name", () => {
    expect(segmentPrefixMatch("up", "up")).toBe(true);
  });

  it("is case-insensitive", () => {
    expect(segmentPrefixMatch("GoGc", "go_gc_total")).toBe(true);
  });

  it("handles underscores in query as segment separators", () => {
    expect(segmentPrefixMatch("go_tot", "go_gc_cleanups_total")).toBe(true);
  });

  it("handles empty query", () => {
    expect(segmentPrefixMatch("", "anything")).toBe(true);
  });

  it("requires backtracking — greedy fails on this", () => {
    // Greedy would consume 'c' from "gc", but it needs to start "cleanups"
    expect(segmentPrefixMatch("gcl", "go_gc_cleanups")).toBe(true);
  });

  it("matches function names without underscores", () => {
    expect(segmentPrefixMatch("hist", "histogram_quantile")).toBe(true);
  });

  it("matches multi-char prefixes across segments", () => {
    expect(segmentPrefixMatch("contcpu", "container_cpu_usage_seconds_total")).toBe(true);
  });
});

describe("getTokenBounds", () => {
  it("finds token at cursor position", () => {
    expect(getTokenBounds("rate(up)", 7)).toEqual({ start: 5, end: 7 });
  });

  it("finds token at start of string", () => {
    expect(getTokenBounds("up", 2)).toEqual({ start: 0, end: 2 });
  });

  it("returns empty bounds at non-word character", () => {
    expect(getTokenBounds("up{", 3)).toEqual({ start: 3, end: 3 });
  });

  it("handles cursor in middle of token", () => {
    expect(getTokenBounds("container_cpu", 5)).toEqual({ start: 0, end: 13 });
  });
});
