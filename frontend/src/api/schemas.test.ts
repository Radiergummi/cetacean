import { api, resetSchemaDriftReports } from "./client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

/**
 * The boundary validates and warns; it never throws. A dashboard pointed at a
 * server it disagrees with renders exactly what it rendered before — the point
 * is that the reason now reaches the console instead of being inferred from a
 * blank cell.
 */

const mockFetch = vi.fn<(...args: unknown[]) => unknown>();
const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

function jsonResponse(data: unknown, headers?: Record<string, string>) {
  return Promise.resolve({
    ok: true,
    status: 200,
    statusText: "OK",
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
    headers: new Headers(headers),
  });
}

/** The issue list from the nth report, which is the second console argument. */
function issuesOf(call: number): string[] {
  const issues = consoleError.mock.calls[call]?.[1];

  return Array.isArray(issues) ? (issues as string[]) : [];
}

function node(overrides: Record<string, unknown> = {}) {
  return {
    ID: "n1",
    Version: { Index: 1 },
    Spec: { Labels: null },
    Description: { Platform: {}, Resources: {}, Engine: {} },
    Status: { State: "ready" },
    ...overrides,
  };
}

beforeEach(() => {
  vi.stubGlobal("fetch", mockFetch);
  mockFetch.mockReset();
  consoleError.mockClear();
  resetSchemaDriftReports();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("response validation", () => {
  it("says nothing about a response that matches", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: node() }));

    await api.node("n1");

    expect(consoleError).not.toHaveBeenCalled();
  });

  it("hands back a mismatched response unchanged rather than throwing", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: node({ ID: 42 }) }));

    const { data } = await api.node("n1");

    expect(data.ID).toBe(42 as unknown as string);
  });

  it("names the endpoint and the field that drifted", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: node({ ID: 42 }) }));

    await api.node("n1");

    expect(consoleError).toHaveBeenCalledTimes(1);

    expect(String(consoleError.mock.calls[0]?.[0])).toContain("/nodes/n1");
    expect(issuesOf(0).join(" ")).toContain("node.ID");
  });

  it("accepts a field the server added that the dashboard does not know", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: node({ SomethingNew: true }) }));

    await api.node("n1");

    expect(consoleError).not.toHaveBeenCalled();
  });

  it("reports a drifted endpoint once, however often a page polls it", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: node({ ID: 42 }) }));

    await api.node("n1");
    await api.node("n1");
    await api.node("n1");

    expect(consoleError).toHaveBeenCalledTimes(1);
  });

  it("groups a query string with the endpoint it belongs to", async () => {
    mockFetch.mockReturnValue(jsonResponse({ query: "web", results: {}, counts: {} }));

    await api.search("web");
    await api.search("other");

    expect(consoleError).toHaveBeenCalledTimes(1);
    expect(consoleError.mock.calls[0]?.[0]).toContain("/search");
  });

  it("checks the list envelope, which is what pagination reads", async () => {
    mockFetch.mockReturnValue(
      jsonResponse({ items: [], limit: 50, offset: 0 }, { "Content-Range": "items 0-0/0" }),
    );

    await api.nodes();

    expect(consoleError).toHaveBeenCalledTimes(1);
    expect(issuesOf(0).join(" ")).toContain("total");
  });

  it("checks the items inside the envelope too", async () => {
    mockFetch.mockReturnValue(
      jsonResponse({ items: [node({ Status: {} })], total: 1, limit: 50, offset: 0 }),
    );

    await api.nodes();

    expect(issuesOf(0).join(" ")).toContain("items.0.Status.State");
  });
});
