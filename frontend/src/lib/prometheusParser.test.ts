import type { PrometheusResponse } from "../api/types";
import { buildInstanceFilter, parseInstant, parseRange } from "./prometheusParser";
import { describe, expect, it } from "vitest";

describe("parseInstant", () => {
  it("returns null for null response", () => {
    expect(parseInstant(null, "name")).toBeNull();
  });

  it("returns null for empty results", () => {
    const response: PrometheusResponse = {
      data: { resultType: "vector", result: [] },
    };
    expect(parseInstant(response, "name")).toBeNull();
  });

  it("returns [label, value] pairs for valid response", () => {
    const response: PrometheusResponse = {
      data: {
        resultType: "vector",
        result: [
          { metric: { name: "cpu" }, value: [1000, "0.75"] },
          { metric: { name: "mem" }, value: [1000, "128"] },
        ],
      },
    };
    expect(parseInstant(response, "name")).toEqual([
      ["cpu", 0.75],
      ["mem", 128],
    ]);
  });
});

describe("parseRange", () => {
  it("returns null for null response", () => {
    expect(parseRange(null, "name")).toBeNull();
  });

  it("returns null for empty results", () => {
    const response: PrometheusResponse = {
      data: { resultType: "matrix", result: [] },
    };
    expect(parseRange(response, "name")).toBeNull();
  });

  it("returns [label, values[]] pairs for valid response", () => {
    const response: PrometheusResponse = {
      data: {
        resultType: "matrix",
        result: [
          {
            metric: { name: "cpu" },
            values: [
              [1000, "0.5"],
              [1001, "0.6"],
            ],
          },
        ],
      },
    };
    expect(parseRange(response, "name")).toEqual([["cpu", [0.5, 0.6]]]);
  });
});

describe("buildInstanceFilter", () => {
  it("returns exact match when instance is provided", () => {
    expect(buildInstanceFilter("10.0.0.1:9100", "", "")).toBe('instance="10.0.0.1:9100"');
  });

  it("returns regex match when address is provided without instance", () => {
    expect(buildInstanceFilter("", "10.0.0.1", "")).toBe('instance=~"10.0.0.1:.*"');
  });

  it("returns regex with FQDN pattern when only hostname is provided", () => {
    expect(buildInstanceFilter("", "", "worker1")).toBe('instance=~"worker1(\\..+)?:.*"');
  });

  it("returns empty string when all inputs are empty", () => {
    expect(buildInstanceFilter("", "", "")).toBe("");
  });

  it("instance takes priority over address and hostname", () => {
    expect(buildInstanceFilter("exact:9100", "10.0.0.1", "worker1")).toBe('instance="exact:9100"');
  });
});
