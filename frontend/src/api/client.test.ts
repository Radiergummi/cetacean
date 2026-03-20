import { api } from "./client";
import { describe, it, expect, vi, beforeEach } from "vitest";

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
    mockFetch.mockReturnValue(jsonResponse({ items: [{ ID: "n1" }], total: 1 }));
    const result = await api.nodes();
    expect(result).toEqual({ items: [{ ID: "n1" }], total: 1 });
    expect(mockFetch).toHaveBeenCalledWith(
      "/nodes",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("fetches a single node (unwraps JSON-LD wrapper)", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: { ID: "n1" } }));
    const result = await api.node("n1");
    expect(result).toEqual({ ID: "n1" });
    expect(mockFetch).toHaveBeenCalledWith(
      "/nodes/n1",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("fetches cluster snapshot", async () => {
    const snapshot = { nodeCount: 3, serviceCount: 5 };
    mockFetch.mockReturnValue(jsonResponse(snapshot));
    const result = await api.cluster();
    expect(result).toEqual(snapshot);
  });

  it("throws on non-ok response with problem detail", async () => {
    mockFetch.mockReturnValue(jsonResponse({ detail: "node not found" }, 404));
    await expect(api.nodes()).rejects.toThrow("node not found");
  });

  it("throws on non-ok response with status text fallback", async () => {
    mockFetch.mockReturnValue(jsonResponse(null, 404));
    await expect(api.nodes()).rejects.toThrow("404 Not Found");
  });

  it("fetches service logs as JSON", async () => {
    const resp = {
      lines: [{ timestamp: "t1", message: "hello", stream: "stdout" }],
      oldest: "t1",
      newest: "t1",
    };
    mockFetch.mockReturnValue(jsonResponse(resp));
    const result = await api.serviceLogs("svc1", { limit: 100 });
    expect(result).toEqual(resp);
    expect(mockFetch).toHaveBeenCalledWith(
      "/services/svc1/logs?limit=100",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("fetches service logs with stream filter", async () => {
    mockFetch.mockReturnValue(jsonResponse({ lines: [], oldest: "", newest: "" }));
    await api.serviceLogs("svc1", { limit: 100, stream: "stderr" });
    expect(mockFetch).toHaveBeenCalledWith(
      "/services/svc1/logs?limit=100&stream=stderr",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("builds metrics query params", async () => {
    mockFetch.mockReturnValue(jsonResponse({ status: "success" }));
    await api.metricsQuery("up", "1234");
    expect(mockFetch).toHaveBeenCalledWith(
      "/metrics?query=up&time=1234",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("builds metrics range query params", async () => {
    mockFetch.mockReturnValue(jsonResponse({ status: "success" }));
    await api.metricsQueryRange("up", "100", "200", "15s");
    expect(mockFetch).toHaveBeenCalledWith(
      "/metrics?query=up&start=100&end=200&step=15s",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("builds service logs stream URL with after param", () => {
    const url = api.serviceLogsStreamURL("svc1", { after: "2024-01-01T00:00:00Z" });
    expect(url).toBe("/services/svc1/logs?after=2024-01-01T00%3A00%3A00Z");
  });

  it("builds service logs stream URL with stream filter", () => {
    const url = api.serviceLogsStreamURL("svc1", {
      after: "2024-01-01T00:00:00Z",
      stream: "stdout",
    });
    expect(url).toBe("/services/svc1/logs?after=2024-01-01T00%3A00%3A00Z&stream=stdout");
  });

  it("builds task logs stream URL without params", () => {
    const url = api.taskLogsStreamURL("t1");
    expect(url).toBe("/tasks/t1/logs");
  });
});
