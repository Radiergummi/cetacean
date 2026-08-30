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

  it("SSE bumps total when an unknown item is created", async () => {
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
        action: "create",
        id: "99",
        resource: { ID: "99", Name: "new-unknown" },
      }),
    );

    await waitFor(() => expect(result.current.total).toBe(6));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data[0]?.ID).toBe("1");
  });

  it("SSE appends unknown items to a fully loaded list", async () => {
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
    expect(result.current.hasMore).toBe(false);

    const created = { ID: "3", Name: "c" };
    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "create",
        id: "3",
        resource: created,
      }),
    );

    await waitFor(() => expect(result.current.total).toBe(3));

    // The list held every item, so the new one belongs on screen. Bumping the
    // total without it would leave a page's worth of items unaccounted for,
    // and the table's load-more sentinel would fetch an offset that overlaps
    // what is already loaded — rendering duplicate rows.
    expect(result.current.data).toHaveLength(3);
    expect(result.current.data).toContainEqual(created);
    expect(result.current.hasMore).toBe(false);
  });

  it("SSE updates to unloaded items leave the total alone", async () => {
    // A partially loaded list can't tell whether an item it doesn't hold is
    // new or simply on a page it hasn't fetched — so it trusts the action.
    // Counting every update as an arrival inflates the total without bound on
    // a churning cluster, and the table's load-more sentinel then walks the
    // offset past the real end of the collection and gets a 416 back.
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(makeFetchResult([{ ID: "1", Name: "a" }], 50));

    const { result } = renderHook(
      () => useSwarmQuery(["tasks"], fetchFn, "task", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.total).toBe(50);

    for (const id of ["90", "91", "92"]) {
      act(() =>
        MockEventSource.instance.simulateEvent("task", {
          type: "task",
          action: "update",
          id,
          resource: { ID: id, Name: `task-${id}` },
        }),
      );
    }

    expect(result.current.total).toBe(50);
    expect(result.current.data).toHaveLength(1);
  });

  it("SSE removes an unloaded item from the total", async () => {
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(makeFetchResult([{ ID: "1", Name: "a" }], 50));

    const { result } = renderHook(
      () => useSwarmQuery(["tasks"], fetchFn, "task", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() =>
      MockEventSource.instance.simulateEvent("task", {
        type: "task",
        action: "remove",
        id: "42",
      }),
    );

    await waitFor(() => expect(result.current.total).toBe(49));
    expect(result.current.data).toHaveLength(1);
  });

  it("SSE leaves a filtered collection's total alone for items it never held", async () => {
    // The stream carries every resource of this type, not only the ones the
    // search selected, so a filtered list cannot tell whether the departing
    // item was ever a member. Counting the unrelated churn would drive the
    // total below the real count — negative, on a busy cluster — and the
    // load-more sentinel would then stop asking for the pages holding the
    // rows that do match.
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(makeFetchResult([{ ID: "1", Name: "nginx-1" }], 3));

    const { result } = renderHook(
      () => useSwarmQuery(["tasks"], fetchFn, "task", ({ ID }: Item) => ID, true),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.total).toBe(3);

    for (const id of ["40", "41", "42", "43", "44"]) {
      act(() =>
        MockEventSource.instance.simulateEvent("task", {
          type: "task",
          action: "remove",
          id,
        }),
      );
    }

    expect(result.current.total).toBe(3);
    expect(result.current.data).toHaveLength(1);
  });

  it("SSE still removes a loaded item from a filtered collection", async () => {
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(
        makeFetchResult(
          [
            { ID: "1", Name: "nginx-1" },
            { ID: "2", Name: "nginx-2" },
          ],
          2,
        ),
      );

    const { result } = renderHook(
      () => useSwarmQuery(["tasks"], fetchFn, "task", ({ ID }: Item) => ID, true),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() =>
      MockEventSource.instance.simulateEvent("task", {
        type: "task",
        action: "remove",
        id: "1",
      }),
    );

    await waitFor(() => expect(result.current.data).toEqual([{ ID: "2", Name: "nginx-2" }]));
    expect(result.current.total).toBe(1);
  });

  it("SSE does not add an unknown creation to a filtered collection", async () => {
    // A fully loaded unfiltered list would show the arrival; a filtered one
    // cannot tell whether it matches the search, and appending it would render
    // a row the filter excludes.
    const fetchFn = vi
      .fn<(offset: number, signal: AbortSignal) => Promise<ReturnType<typeof makeFetchResult>>>()
      .mockResolvedValue(makeFetchResult([{ ID: "1", Name: "nginx-1" }], 1));

    const { result } = renderHook(
      () => useSwarmQuery(["services"], fetchFn, "service", ({ ID }: Item) => ID, true),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "create",
        id: "99",
        resource: { ID: "99", Name: "redis" },
      }),
    );

    expect(result.current.total).toBe(1);
    expect(result.current.data).toEqual([{ ID: "1", Name: "nginx-1" }]);
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
