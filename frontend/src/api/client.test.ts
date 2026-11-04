import { api, currentETag } from "./client";
import { describe, it, expect, vi, beforeEach } from "vitest";

const mockFetch = vi.fn<(...args: unknown[]) => unknown>();

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
    headers: new Headers(),
  });
}

describe("api client", () => {
  // Every detail response names itself, already spelled with the canonical ID,
  // so a page reached by name learns the ID without knowing where its type
  // keeps one.
  it("lifts a detail response's @id into canonicalPath", async () => {
    mockFetch.mockReturnValue(
      jsonResponse({
        "@context": "/api/context.jsonld",
        "@id": "/nodes/n0d3id",
        "@type": "Node",
        node: { ID: "n0d3id" },
      }),
    );

    const result = await api.node("worker-2");

    expect(result.canonicalPath).toBe("/nodes/n0d3id");
  });

  it("reports no canonicalPath for a response carrying no @id", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: { ID: "n0d3id" } }));

    const result = await api.node("worker-2");

    expect(result.canonicalPath).toBeUndefined();
  });

  it("treats a 416 range response as the end of the collection", async () => {
    // The server answers 416 when the requested offset is past the end. That
    // is a correct answer to a stale question — the client's idea of the
    // total drifted — not a failure worth tearing the page down for. Content-
    // Range carries the real total, so the page resyncs from it.
    mockFetch.mockReturnValue(
      Promise.resolve({
        ok: false,
        status: 416,
        statusText: "Requested Range Not Satisfiable",
        json: () => Promise.resolve({ title: "range start is beyond the total" }),
        text: () => Promise.resolve(""),
        headers: new Headers({ "Content-Range": "items */12" }),
      }),
    );

    const result = await api.tasks({ offset: 50 });

    expect(result.data).toEqual({ items: [], total: 12, limit: 50, offset: 50 });
  });

  it("fetches nodes with Range header", async () => {
    mockFetch.mockReturnValue(
      jsonResponse({ items: [{ ID: "n1" }], total: 1, limit: 50, offset: 0 }),
    );
    const result = await api.nodes();
    expect(result).toEqual({
      data: { items: [{ ID: "n1" }], total: 1, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
    expect(mockFetch).toHaveBeenCalledWith(
      "/nodes",
      expect.objectContaining({
        headers: { Accept: "application/json", Range: "items=0-49" },
      }),
    );
  });

  it("sends Range header with query params for nodes list", async () => {
    mockFetch.mockReturnValue(jsonResponse({ items: [], total: 0, limit: 50, offset: 0 }));
    await api.nodes({ sort: "hostname", dir: "desc", search: "web" });
    expect(mockFetch).toHaveBeenCalledWith(
      "/nodes?sort=hostname&dir=desc&search=web",
      expect.objectContaining({
        headers: { Accept: "application/json", Range: "items=0-49" },
      }),
    );
  });

  it("sends custom Range offset", async () => {
    mockFetch.mockReturnValue(jsonResponse({ items: [], total: 100, limit: 50, offset: 50 }));
    await api.nodes({ offset: 50 });
    expect(mockFetch).toHaveBeenCalledWith(
      "/nodes",
      expect.objectContaining({
        headers: { Accept: "application/json", Range: "items=50-99" },
      }),
    );
  });

  it("pages through every version of one secret group", async () => {
    const versions = (from: number, count: number) =>
      Array.from({ length: count }, (_, index) => ({
        ID: `s${from + index}`,
        Version: { Index: 1 },
        Spec: { Name: `db_v${from + index}` },
        UsedBy: 0,
      }));
    mockFetch
      .mockReturnValueOnce(
        jsonResponse({ items: versions(0, 200), total: 230, limit: 200, offset: 0 }),
      )
      .mockReturnValueOnce(
        jsonResponse({ items: versions(200, 30), total: 230, limit: 200, offset: 200 }),
      );

    expect(await api.secretVersions("s0")).toHaveLength(230);
    expect(mockFetch).toHaveBeenLastCalledWith("/secrets?versionsOf=s0", expect.anything());
  });

  it("pages through every secret for a picker", async () => {
    const secrets = (from: number, count: number) =>
      Array.from({ length: count }, (_, index) => ({
        ID: `s${from + index}`,
        Version: { Index: 1 },
        Spec: { Name: `secret-${from + index}` },
      }));
    mockFetch
      .mockReturnValueOnce(
        jsonResponse({ items: secrets(0, 200), total: 250, limit: 200, offset: 0 }),
      )
      .mockReturnValueOnce(
        jsonResponse({ items: secrets(200, 50), total: 250, limit: 200, offset: 200 }),
      );

    const all = await api.allSecrets();

    expect(all).toHaveLength(250);
    expect(mockFetch).toHaveBeenCalledTimes(2);
    // A picker offers every version, not only the newest of each group.
    expect(mockFetch).toHaveBeenLastCalledWith(
      "/secrets?versions=all",
      expect.objectContaining({
        headers: { Accept: "application/json", Range: "items=200-399" },
      }),
    );
  });

  it("pages through every service matching a filter", async () => {
    const services = (from: number, count: number) =>
      Array.from({ length: count }, (_, index) => ({
        ID: `svc${from + index}`,
        Version: { Index: 1 },
        Spec: { Name: `web_${from + index}`, Mode: { Replicated: { Replicas: 1 } } },
        RunningTasks: 1,
      }));
    mockFetch
      .mockReturnValueOnce(
        jsonResponse({ items: services(0, 200), total: 260, limit: 200, offset: 0 }),
      )
      .mockReturnValueOnce(
        jsonResponse({ items: services(200, 60), total: 260, limit: 200, offset: 200 }),
      );

    const all = await api.allServices({ filter: 'stack == "web"' });

    expect(all).toHaveLength(260);
    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(mockFetch).toHaveBeenLastCalledWith(
      `/services?${new URLSearchParams({ filter: 'stack == "web"' })}`,
      expect.anything(),
    );
  });

  it("stops paging when the server returns an empty page short of the total", async () => {
    mockFetch.mockReturnValue(jsonResponse({ items: [], total: 10, limit: 200, offset: 0 }));

    expect(await api.allNetworks()).toEqual([]);
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it("accepts 206 Partial Content as success", async () => {
    mockFetch.mockReturnValue(
      jsonResponse({ items: [{ ID: "n1" }], total: 100, limit: 50, offset: 0 }, 206),
    );
    const result = await api.nodes();
    expect(result).toEqual({
      data: { items: [{ ID: "n1" }], total: 100, limit: 50, offset: 0 },
      allowedMethods: new Set(),
    });
  });

  it("sends filter query param", async () => {
    mockFetch.mockReturnValue(jsonResponse({ items: [], total: 0, limit: 50, offset: 0 }));
    await api.services({ filter: "Spec.Name contains 'web'" });
    expect(mockFetch).toHaveBeenCalledWith(expect.stringContaining("filter="), expect.anything());
  });

  it("fetches a single node (unwraps JSON-LD wrapper)", async () => {
    mockFetch.mockReturnValue(jsonResponse({ node: { ID: "n1" } }));
    const result = await api.node("n1");
    expect(result).toEqual({
      data: { ID: "n1" },
      allowedMethods: new Set(),
      editable: new Set(),
      annotatable: false,
    });
    expect(mockFetch).toHaveBeenCalledWith(
      "/nodes/n1",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("encodes the stack name into its path", async () => {
    mockFetch.mockReturnValue(
      jsonResponse({
        stack: { name: "a#b", services: [], configs: [], secrets: [], networks: [], volumes: [] },
      }),
    );

    await api.stack("a#b");

    expect(mockFetch).toHaveBeenCalledWith("/stacks/a%23b", expect.anything());
  });

  it("fetches cluster snapshot", async () => {
    const snapshot = { nodeCount: 3, serviceCount: 5 };
    mockFetch.mockReturnValue(jsonResponse(snapshot));
    const { data } = await api.cluster();
    expect(data).toEqual(snapshot);
  });

  it("throws on non-ok response with problem detail", async () => {
    const response = {
      ok: false,
      status: 404,
      statusText: "Not Found",
      json: () => Promise.resolve({ detail: "node not found" }),
      headers: new Headers(),
    };
    mockFetch.mockReturnValue(Promise.resolve(response));
    await expect(api.nodes()).rejects.toThrow("node not found");
  });

  it("throws on non-ok response with status text fallback", async () => {
    const response = {
      ok: false,
      status: 404,
      statusText: "Not Found",
      json: () => Promise.resolve(null),
      headers: new Headers(),
    };
    mockFetch.mockReturnValue(Promise.resolve(response));
    await expect(api.nodes()).rejects.toThrow("Not Found");
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

describe("volume paths", () => {
  it("encodes the name of the volume it reads", async () => {
    mockFetch.mockReturnValue(jsonResponse({ volume: { Name: "a b" }, services: [] }));

    await api.volume("data/a b");

    expect(mockFetch).toHaveBeenCalledWith("/volumes/data%2Fa%20b", expect.anything());
  });

  it("encodes the name of the volume it removes", async () => {
    mockFetch.mockReturnValue(Promise.resolve({ ok: true, status: 204, headers: new Headers() }));

    await api.removeVolume("data/a b");

    expect(mockFetch).toHaveBeenCalledWith("/volumes/data%2Fa%20b", expect.anything());
  });
});

describe("preconditions", () => {
  it("reads the current ETag of a path", async () => {
    mockFetch.mockReturnValue(
      Promise.resolve({ ok: true, status: 200, headers: new Headers({ ETag: '"abc"' }) }),
    );

    expect(await currentETag("/services/svc1/resources")).toBe('"abc"');
    expect(mockFetch).toHaveBeenCalledWith(
      "/services/svc1/resources",
      expect.objectContaining({ method: "HEAD" }),
    );
  });

  it("sends If-Match on a write when given one", async () => {
    mockFetch.mockReturnValue(jsonResponse({ resources: {} }));

    await api.patchServiceResources("svc1", { Limits: {} }, '"abc"');

    const [, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(new Headers(init.headers).get("If-Match")).toBe('"abc"');
  });

  it("sends no If-Match when there is none", async () => {
    mockFetch.mockReturnValue(jsonResponse({ resources: {} }));

    await api.patchServiceResources("svc1", { Limits: {} });

    const [, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(new Headers(init.headers).has("If-Match")).toBe(false);
  });

  it("says to reload when the resource changed since it was read", async () => {
    mockFetch.mockReturnValue(
      jsonResponse({ title: "Precondition Failed", detail: "the ETag does not match" }, 412),
    );

    await expect(api.patchServiceResources("svc1", {}, '"old"')).rejects.toThrow(/reload/i);
  });
});

describe("api.historyPage", () => {
  const collection = {
    "@context": "/api/context.jsonld",
    items: [],
    total: 0,
    limit: 2,
    offset: 0,
  };

  it("follows the next link the server sends", async () => {
    mockFetch.mockReturnValue(
      Promise.resolve({
        ok: true,
        status: 200,
        url: "http://localhost/history?limit=2",
        json: () => Promise.resolve(collection),
        headers: new Headers({ Link: '</history?limit=2&before=4>; rel="next"' }),
      }),
    );

    const { next } = await api.historyPage("/history?limit=2");

    expect(next).toBe("/history?limit=2&before=4");
  });

  it("has no next page without a next link", async () => {
    mockFetch.mockReturnValue(jsonResponse(collection));

    const { next } = await api.historyPage("/history?limit=2");

    expect(next).toBeUndefined();
  });
});

describe("editable sub-resources", () => {
  it("names the direct children a response links with rel=edit, after a redirect", async () => {
    mockFetch.mockReturnValue(
      Promise.resolve({
        ok: true,
        status: 200,
        url: "http://localhost/services/abc123",
        json: () => Promise.resolve({ service: { ID: "abc123" } }),
        headers: new Headers({
          Link: [
            '</services/abc123/scale>; rel="edit"',
            '</services/abc123/labels>; rel="edit"',
            '</services/other/scale>; rel="edit"',
            '</services/abc123/a/b>; rel="edit"',
            '</services/abc123/logs>; rel="alternate"',
          ].join(", "),
        }),
      }),
    );

    const { editable } = await api.service("web");

    expect(editable).toEqual(new Set(["scale", "labels"]));
  });
});

describe("api.recentActivity", () => {
  const page = (items: unknown[]) =>
    jsonResponse({
      "@context": "/api/context.jsonld",
      items,
      total: items.length,
      limit: 10,
      offset: 0,
    });

  const entry = (id: number, kind: string, timestamp: string) => ({
    id,
    kind,
    timestamp,
    type: kind === "gap" ? "" : "service",
    action: kind === "gap" ? "" : "update",
    resourceId: kind === "gap" ? "" : "svc1",
    name: kind === "gap" ? "" : "web",
  });

  const route = (pages: Record<string, unknown[]>) =>
    mockFetch.mockImplementation((url) =>
      page(pages[new URL(String(url), "http://localhost").searchParams.get("kind") ?? ""] ?? []),
    );

  const requests = () =>
    new Map(
      mockFetch.mock.calls.map(([url]) => {
        const parsed = new URL(String(url), "http://localhost");

        return [parsed.searchParams.get("kind"), parsed.searchParams] as const;
      }),
    );

  it("asks for the changes, then the gaps since the oldest, and merges them newest first", async () => {
    route({
      change: [
        entry(9, "change", "2026-10-30T17:00:00Z"),
        entry(4, "change", "2026-10-30T12:00:00Z"),
      ],
      gap: [entry(7, "gap", "2026-10-30T13:00:00Z")],
    });

    const entries = await api.recentActivity({ resourceId: "svc1", type: "service" }, 2);

    expect(entries.map(({ id }) => id)).toEqual([9, 7, 4]);

    const sent = requests();

    expect(sent.get("change")?.get("resourceId")).toBe("svc1");
    expect(sent.get("change")?.get("limit")).toBe("2");
    expect(sent.get("gap")?.get("since")).toBe("2026-10-30T12:00:00Z");
  });

  it("merges the newest changes and annotations", async () => {
    route({
      change: [
        entry(9, "change", "2026-10-30T17:00:00Z"),
        entry(7, "change", "2026-10-30T15:00:00Z"),
        entry(2, "change", "2026-10-30T10:00:00Z"),
      ],
      annotation: [
        { ...entry(8, "annotation", "2026-10-30T11:00:00Z"), type: "", text: "note" },
        { ...entry(1, "annotation", "2026-10-30T09:00:00Z"), type: "", text: "old" },
      ],
      gap: [entry(6, "gap", "2026-10-30T14:00:00Z")],
    });

    const entries = await api.recentActivity({ resourceId: "svc1", type: "service" }, 3);

    expect(entries.map(({ id }) => id)).toEqual([9, 8, 7, 6]);

    const sent = requests();

    expect(sent.get("annotation")?.get("resourceId")).toBe("svc1");
    expect(sent.get("annotation")?.get("type")).toBe("service");
    expect(sent.get("annotation")?.get("limit")).toBe("3");
    expect(sent.get("gap")?.get("since")).toBe("2026-10-30T11:00:00Z");
  });

  it("returns annotations when there are no changes", async () => {
    route({
      annotation: [{ ...entry(3, "annotation", "2026-10-30T11:00:00Z"), type: "", text: "note" }],
      gap: [entry(2, "gap", "2026-10-30T12:00:00Z")],
    });

    expect((await api.recentActivity({}, 25)).map(({ id }) => id)).toEqual([3, 2]);
  });

  it("asks for no gaps without a change or an annotation", async () => {
    route({});

    expect(await api.recentActivity({}, 25)).toEqual([]);
    expect(requests().has("gap")).toBe(false);
  });
});

describe("api.timeline", () => {
  const page = (items: unknown[], next?: string) =>
    jsonResponse({
      "@context": "/api/context.jsonld",
      items,
      total: items.length,
      limit: 200,
      offset: 0,
    }).then((response) => {
      if (next) {
        response.headers.set("Link", `<${next}>; rel="next"`);
      }

      return response;
    });

  const entry = (id: number) => ({
    id,
    kind: "change",
    timestamp: "2026-10-30T12:00:00Z",
    type: "service",
    action: "update",
    resourceId: "svc1",
    name: "web",
  });

  const params = { type: "service", resourceId: "svc1", since: "2026-10-30T00:00:00Z" };

  function fullPages() {
    for (let index = 5; index > 0; index--) {
      mockFetch.mockReturnValueOnce(page([entry(index)], `/history?before=${index}&limit=200`));
    }
  }

  it("asks for the service's own entries", async () => {
    mockFetch.mockReturnValueOnce(page([entry(1)]));

    const { truncated } = await api.timeline(params);
    const url = new URL(String(mockFetch.mock.calls[0]?.[0]), "http://localhost");

    expect(url.searchParams.get("tasks")).toBe("false");
    expect(truncated).toBe(false);
  });

  it("is not truncated when nothing is left past the last page", async () => {
    fullPages();
    mockFetch.mockReturnValueOnce(page([]));

    const { entries, truncated } = await api.timeline(params);

    expect(entries).toHaveLength(5);
    expect(truncated).toBe(false);
    expect(
      new URL(String(mockFetch.mock.calls[5]?.[0]), "http://localhost").searchParams.get("limit"),
    ).toBe("1");
  });

  it("is truncated when an entry is left past the last page", async () => {
    fullPages();
    mockFetch.mockReturnValueOnce(page([entry(0)]));

    expect((await api.timeline(params)).truncated).toBe(true);
  });
});

describe("annotation capabilities", () => {
  const headed = (headers: Record<string, string>, url = "http://localhost/services/x") =>
    Promise.resolve({
      ok: true,
      status: 200,
      url,
      json: () => Promise.resolve({ service: { ID: "x" } }),
      headers: new Headers(headers),
    });

  it("reads whether a response links annotations for editing", async () => {
    mockFetch.mockReturnValueOnce(
      headed({ Link: '</annotations>; rel="edit", </services/x/scale>; rel="edit"' }),
    );

    const linked = await api.service("x");

    expect(linked.annotatable).toBe(true);
    expect(linked.editable).toEqual(new Set(["scale"]));

    mockFetch.mockReturnValueOnce(headed({ Link: '</services/x/scale>; rel="edit"' }));

    expect((await api.service("x")).annotatable).toBe(false);
  });

  it("reads from a list page whether the caller may create an annotation", async () => {
    const page = (link: string) =>
      Promise.resolve({
        ok: true,
        status: 200,
        url: "http://localhost/history",
        json: () => Promise.resolve({ items: [], total: 0, limit: 50, offset: 0 }),
        headers: new Headers({ Link: link }),
      });

    mockFetch.mockReturnValueOnce(page('</annotations>; rel="edit"'));

    expect((await api.historyPage("/history")).annotatable).toBe(true);

    mockFetch.mockReturnValueOnce(page(""));

    expect((await api.historyPage("/history")).annotatable).toBe(false);
  });

  it("creates an annotation", async () => {
    mockFetch.mockReturnValueOnce(jsonResponse({ id: 7, kind: "annotation" }, 201));

    const body = { text: "load test", tags: [], timestamp: "2026-11-02T10:00:00Z" };

    expect(await api.createAnnotation(body)).toEqual({ id: 7, kind: "annotation" });

    const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];

    expect(url).toBe("/annotations");
    expect(init.method).toBe("POST");
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
    expect(JSON.parse(String(init.body))).toEqual(body);
  });

  it("reads one annotation with what the caller may do and its version", async () => {
    mockFetch.mockReturnValueOnce(
      Promise.resolve({
        ok: true,
        status: 200,
        url: "http://localhost/annotations/7",
        json: () =>
          Promise.resolve({
            "@id": "/annotations/7",
            "@type": "Annotation",
            annotation: { id: 7, text: "load test" },
          }),
        headers: new Headers({ Allow: "GET, HEAD, PATCH, DELETE", ETag: '"12"' }),
      }),
    );

    const { data, allowedMethods, etag } = await api.annotation(7);

    expect(data).toEqual({ id: 7, text: "load test" });
    expect(allowedMethods).toEqual(new Set(["GET", "HEAD", "PATCH", "DELETE"]));
    expect(etag).toBe('"12"');
    expect(mockFetch.mock.calls[0]?.[0]).toBe("/annotations/7");
  });

  it("patches an annotation as a merge patch on its version", async () => {
    mockFetch.mockReturnValueOnce(jsonResponse({ id: 7 }));

    await api.updateAnnotation(7, { until: null }, '"12"');

    const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
    const sent = new Headers(init.headers);

    expect(url).toBe("/annotations/7");
    expect(init.method).toBe("PATCH");
    expect(sent.get("Content-Type")).toBe("application/merge-patch+json");
    expect(sent.get("If-Match")).toBe('"12"');
    expect(JSON.parse(String(init.body))).toEqual({ until: null });
  });

  it("deletes an annotation on its version", async () => {
    mockFetch.mockReturnValueOnce(
      Promise.resolve({ ok: true, status: 204, headers: new Headers() }),
    );

    await api.deleteAnnotation(7, '"12"');

    const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];

    expect(url).toBe("/annotations/7");
    expect(init.method).toBe("DELETE");
    expect(new Headers(init.headers).get("If-Match")).toBe('"12"');
  });
});
