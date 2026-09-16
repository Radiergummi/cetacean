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
  } as never);
  vi.mocked(api.history).mockResolvedValue([] as never);
});

function renderForKey(key: string) {
  return renderHook(
    () =>
      useDetailResource<Node>(key, api.node as never, "/nodes", {
        idOf: (node) => node.ID,
      }),
    { wrapper: createWrapper(createTestQueryClient()) },
  );
}

describe("useDetailResource", () => {
  it("requests history for the canonical ID, not the route parameter", async () => {
    renderForKey("worker-2");

    await waitFor(() => expect(api.history).toHaveBeenCalled());

    expect(api.history).toHaveBeenCalledWith(
      expect.objectContaining({ resourceId: "n0d3id" }),
      expect.anything(),
    );
  });

  it("subscribes to the SSE path of the canonical ID", async () => {
    const { result } = renderForKey("worker-2");

    await waitFor(() => expect(result.current.data).not.toBeNull());

    expect(streamPaths.at(-1)).toBe("/nodes/n0d3id");
  });

  // A transport failure exhausts the query's retries and refetchOnWindowFocus
  // is off, so the stream reconnecting is the only thing that revives the page.
  it("still opens a stream when the fetch fails, so the page can recover", async () => {
    vi.mocked(api.node).mockRejectedValue(new Error("server restarting"));

    const { result } = renderForKey("worker-2");

    await waitFor(() => expect(result.current.error).not.toBeNull());

    expect(streamPaths.at(-1)).toBe("/nodes/worker-2");
  });

  it("names neither stream nor history while the first fetch is in flight", async () => {
    let settle = (): void => {};
    vi.mocked(api.node).mockReturnValue(
      new Promise((resolve) => {
        settle = () => resolve({ data: node, allowedMethods: new Set<string>() } as never);
      }) as never,
    );

    renderForKey("worker-2");

    // History is what must wait: it takes its identifier in a query parameter
    // no redirect rewrites, so asking before the ID is known returns nothing.
    expect(api.history).not.toHaveBeenCalled();
    expect(streamPaths.at(-1)).toBe("/nodes/worker-2");

    settle();

    await waitFor(() => expect(api.history).toHaveBeenCalled());
    expect(streamPaths.at(-1)).toBe("/nodes/n0d3id");
  });
});
