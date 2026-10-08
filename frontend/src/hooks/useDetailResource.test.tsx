import { useDetailResource } from "./useDetailResource";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const streamPaths: (string | undefined)[] = [];

vi.mock("./useResourceStream", () => ({
  useResourceStream: (path: string | undefined) => {
    streamPaths.push(path);

    return { connected: true };
  },
}));

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    node: vi.fn<() => Promise<unknown>>(),
    history: vi.fn<() => Promise<unknown>>(),
  },
}));

interface Node {
  ID: string;
  Description: { Hostname: string };
}

const node: Node = { ID: "n0d3id", Description: { Hostname: "worker-2" } };

beforeEach(() => {
  vi.clearAllMocks();
  streamPaths.length = 0;
  vi.mocked(api.node).mockResolvedValue({
    data: node,
    allowedMethods: new Set<string>(),
    canonicalPath: "/nodes/n0d3id",
  } as never);
  vi.mocked(api.history).mockResolvedValue([] as never);
});

function renderForKey(key: string) {
  return renderHook(() => useDetailResource<Node>(key, api.node as never, "/nodes"), {
    wrapper: createWrapper(createTestQueryClient()),
  });
}

describe("useDetailResource", () => {
  // The identifier travels in a query parameter no redirect rewrites, so the
  // server resolves it — but only against a type, which is what must be sent.
  it("asks history for the route parameter and the type to resolve it against", async () => {
    renderForKey("worker-2");

    await waitFor(() => expect(api.history).toHaveBeenCalled());

    expect(api.history).toHaveBeenCalledWith(
      expect.objectContaining({ resourceId: "worker-2", type: "node" }),
      expect.anything(),
    );
  });

  // The page never says where its type keeps an ID: the response names itself,
  // already spelled canonically, and `@id` is that name.
  it("takes the SSE path from the response's own @id", async () => {
    const { result } = renderForKey("worker-2");

    await waitFor(() => expect(result.current.data).not.toBeNull());

    expect(streamPaths.at(-1)).toBe("/nodes/n0d3id");
  });

  // A response outside the resource model carries no @id, and a page that
  // cannot name its stream canonically still has to have one.
  it("keeps the route path when the response carries no @id", async () => {
    vi.mocked(api.node).mockResolvedValue({
      data: node,
      allowedMethods: new Set<string>(),
    } as never);

    const { result } = renderForKey("worker-2");

    await waitFor(() => expect(result.current.data).not.toBeNull());

    expect(streamPaths.at(-1)).toBe("/nodes/worker-2");
  });

  // A transport failure exhausts the query's retries and refetchOnWindowFocus
  // is off, so the stream reconnecting is the only thing that revives the page.
  it("still opens a stream when the fetch fails, so the page can recover", async () => {
    vi.mocked(api.node).mockRejectedValue(new Error("server restarting"));

    const { result } = renderForKey("worker-2");

    await waitFor(() => expect(result.current.error).not.toBeNull());

    expect(streamPaths.at(-1)).toBe("/nodes/worker-2");
  });

  // Serializing history behind the fetch would cost a round-trip on every
  // detail page to serve the name-addressed load, which is the rare one.
  it("asks for history without waiting for the first fetch to answer", async () => {
    let settle = (): void => {};
    vi.mocked(api.node).mockReturnValue(
      new Promise((resolve) => {
        settle = () =>
          resolve({
            data: node,
            allowedMethods: new Set<string>(),
            canonicalPath: "/nodes/n0d3id",
          } as never);
      }) as never,
    );

    renderForKey("worker-2");

    await waitFor(() => expect(api.history).toHaveBeenCalled());
    expect(streamPaths.at(-1)).toBe("/nodes/worker-2");

    settle();

    await waitFor(() => expect(streamPaths.at(-1)).toBe("/nodes/n0d3id"));
  });
});
