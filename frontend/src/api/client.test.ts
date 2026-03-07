import { describe, it, expect, vi, beforeEach } from "vitest";
import { api } from "./client";

const mockFetch = vi.fn();

beforeEach(() => {
  vi.stubGlobal("fetch", mockFetch);
  mockFetch.mockReset();
});

function jsonResponse(data: unknown, status = 200) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? "OK" : "Not Found",
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(typeof data === "string" ? data : JSON.stringify(data)),
  });
}

describe("api client", () => {
  it("fetches nodes", async () => {
    mockFetch.mockReturnValue(jsonResponse([{ ID: "n1" }]));
    const result = await api.nodes();
    expect(result).toEqual([{ ID: "n1" }]);
    expect(mockFetch).toHaveBeenCalledWith("/api/nodes");
  });

  it("fetches a single node", async () => {
    mockFetch.mockReturnValue(jsonResponse({ ID: "n1" }));
    const result = await api.node("n1");
    expect(result).toEqual({ ID: "n1" });
    expect(mockFetch).toHaveBeenCalledWith("/api/nodes/n1");
  });

  it("fetches cluster snapshot", async () => {
    const snapshot = { nodeCount: 3, serviceCount: 5 };
    mockFetch.mockReturnValue(jsonResponse(snapshot));
    const result = await api.cluster();
    expect(result).toEqual(snapshot);
  });

  it("throws on non-ok response", async () => {
    mockFetch.mockReturnValue(jsonResponse(null, 404));
    await expect(api.nodes()).rejects.toThrow("404 Not Found");
  });

  it("fetches service logs as text", async () => {
    mockFetch.mockReturnValue(jsonResponse("log line 1\nlog line 2"));
    const result = await api.serviceLogs("svc1", 100);
    expect(result).toBe("log line 1\nlog line 2");
    expect(mockFetch).toHaveBeenCalledWith("/api/services/svc1/logs?tail=100");
  });

  it("builds metrics query params", async () => {
    mockFetch.mockReturnValue(jsonResponse({ status: "success" }));
    await api.metricsQuery("up", "1234");
    expect(mockFetch).toHaveBeenCalledWith("/api/metrics/query?query=up&time=1234");
  });

  it("builds metrics range query params", async () => {
    mockFetch.mockReturnValue(jsonResponse({ status: "success" }));
    await api.metricsQueryRange("up", "100", "200", "15s");
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/metrics/query_range?query=up&start=100&end=200&step=15s",
    );
  });

  it("builds service logs stream URL with follow and since", async () => {
    mockFetch.mockReturnValue(jsonResponse(""));
    const abort = new AbortController();
    await api.serviceLogsStream("svc1", {
      tail: 0,
      since: "2024-01-01T00:00:00Z",
      signal: abort.signal,
    });
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/services/svc1/logs?follow=true&tail=0&since=2024-01-01T00%3A00%3A00Z",
      { signal: abort.signal },
    );
  });
});
