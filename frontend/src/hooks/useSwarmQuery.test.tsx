import { MockEventSource, createTestQueryClient, createWrapper } from "../test/mocks";
import { useSwarmQuery } from "./useSwarmQuery";
import { QueryClient } from "@tanstack/react-query";
import { renderHook, waitFor, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

interface Item {
  ID: string;
  Name: string;
}

let testQueryClient: QueryClient;

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  testQueryClient = createTestQueryClient();
});

afterEach(() => {
  testQueryClient.clear();
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return createWrapper(testQueryClient, { withRouter: false })({ children });
}

function makeFetchResult(items: Item[], total: number, offset = 0) {
  return {
    data: { items, total, limit: 50, offset },
    allowedMethods: new Set(["GET", "HEAD"]),
  };
}

describe("useSwarmQuery", () => {
  it("fetches initial data", async () => {
    const items: Item[] = [{ ID: "1", Name: "svc1" }];
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(makeFetchResult(items, 1));

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual(items);
    expect(result.current.total).toBe(1);
    expect(result.current.error).toBeNull();
  });

  it("exposes loadMore and hasMore for pagination", async () => {
    const fetchFn =
      vi.fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>();
    fetchFn
      .mockResolvedValueOnce(
        makeFetchResult(
          [
            { ID: "1", Name: "a" },
            { ID: "2", Name: "b" },
          ],
          3,
          0,
        ),
      )
      .mockResolvedValueOnce(makeFetchResult([{ ID: "3", Name: "c" }], 3, 2));

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toHaveLength(2);
    expect(result.current.hasMore).toBe(true);
    expect(fetchFn).toHaveBeenCalledWith(0, expect.any(AbortSignal));

    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.loadingMore).toBe(false));
    expect(fetchFn).toHaveBeenCalledWith(2, expect.any(AbortSignal));

    expect(result.current.data).toHaveLength(3);
    expect(result.current.data).toEqual([
      { ID: "1", Name: "a" },
      { ID: "2", Name: "b" },
      { ID: "3", Name: "c" },
    ]);
    expect(result.current.hasMore).toBe(false);
  });

  it("handles fetch errors", async () => {
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockRejectedValue(new Error("fail"));

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.data).toEqual([]);
  });

  it("SSE updates item in-place", async () => {
    const items: Item[] = [{ ID: "1", Name: "old" }];
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(makeFetchResult(items, 1));

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    const updated = { ID: "1", Name: "new" };
    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "1",
        resource: updated,
      }),
    );

    await waitFor(() => expect(result.current.data).toEqual([updated]));
  });

  it("SSE bumps total for unknown items", async () => {
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(makeFetchResult([{ ID: "1", Name: "a" }], 5));

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.total).toBe(5);

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "99",
        resource: { ID: "99", Name: "new-unknown" },
      }),
    );

    await waitFor(() => expect(result.current.total).toBe(6));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data[0].ID).toBe("1");
  });

  it("SSE removes item", async () => {
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(
        makeFetchResult(
          [
            { ID: "1", Name: "a" },
            { ID: "2", Name: "b" },
          ],
          2,
        ),
      );

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "remove",
        id: "1",
      }),
    );

    await waitFor(() => expect(result.current.data).toEqual([{ ID: "2", Name: "b" }]));
    expect(result.current.total).toBe(1);
  });

  it("sync event invalidates queries", async () => {
    const page0 = [
      { ID: "1", Name: "a" },
      { ID: "2", Name: "b" },
    ];
    const refreshed = [
      { ID: "1", Name: "a-refreshed" },
      { ID: "2", Name: "b-refreshed" },
    ];

    const fetchFn =
      vi.fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>();
    fetchFn
      .mockResolvedValueOnce(makeFetchResult(page0, 2))
      .mockResolvedValueOnce(makeFetchResult(refreshed, 2));

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual(page0);

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "sync",
        action: "sync",
        id: "",
      }),
    );

    await waitFor(() => expect(result.current.data).toEqual(refreshed));
    expect(result.current.total).toBe(2);
  });
});
