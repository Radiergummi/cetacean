import { useSwarmResource } from "./useSwarmResource";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

interface Item {
  ID: string;
  Name: string;
}

// Minimal EventSource mock
class MockEventSource {
  static instance: MockEventSource;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;

  constructor(_url: string) {
    MockEventSource.instance = this;
  }
  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }
  close() {
    this.closed = true;
  }
  simulateEvent(type: string, data: unknown) {
    const handlers = this.listeners.get(type) || [];
    const event = new MessageEvent("message", { data: JSON.stringify(data) });
    handlers.forEach((handler) => handler(event));
  }
}

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return <>{children}</>;
}

describe("useSwarmResource", () => {
  it("fetches initial data", async () => {
    const items: Item[] = [{ ID: "1", Name: "svc1" }];
    const fetchFn = vi.fn().mockResolvedValue({ items, total: 1, limit: 50, offset: 0 });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
    );

    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual(items);
    expect(result.current.total).toBe(1);
    expect(result.current.error).toBeNull();
  });

  it("handles fetch errors", async () => {
    const fetchFn = vi.fn().mockRejectedValue(new Error("fail"));

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.data).toEqual([]);
  });

  it("updates item on SSE update event", async () => {
    const items: Item[] = [{ ID: "1", Name: "old" }];
    const fetchFn = vi.fn().mockResolvedValue({ items, total: 1, limit: 50, offset: 0 });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
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

    expect(result.current.data).toEqual([updated]);
  });

  it("bumps sseOffset for SSE event with unknown id", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      items: [{ ID: "1", Name: "a" }],
      total: 1,
      limit: 50,
      offset: 0,
    });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "2",
        resource: { ID: "2", Name: "b" },
      }),
    );

    // Should NOT append to data — item might belong to an unloaded page
    expect(result.current.data).toHaveLength(1);
    // Total should bump via sseOffset
    expect(result.current.total).toBe(2);
  });

  it("removes item on SSE remove event", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      items: [
        { ID: "1", Name: "a" },
        { ID: "2", Name: "b" },
      ],
      total: 2,
      limit: 50,
      offset: 0,
    });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "remove",
        id: "1",
      }),
    );

    expect(result.current.data).toEqual([{ ID: "2", Name: "b" }]);
    expect(result.current.total).toBe(1);
  });

  it("retry re-fetches data", async () => {
    const fetchFn = vi
      .fn()
      .mockRejectedValueOnce(new Error("fail"))
      .mockResolvedValueOnce({
        items: [{ ID: "1", Name: "ok" }],
        total: 1,
        limit: 50,
        offset: 0,
      });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeInstanceOf(Error);

    act(() => result.current.retry());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual([{ ID: "1", Name: "ok" }]);
    expect(result.current.error).toBeNull();
  });

  it("exposes loadMore and hasMore for pagination", async () => {
    const fetchFn = vi
      .fn()
      .mockResolvedValueOnce({
        items: [
          { ID: "1", Name: "a" },
          { ID: "2", Name: "b" },
        ],
        total: 3,
        limit: 50,
        offset: 0,
      })
      .mockResolvedValueOnce({
        items: [{ ID: "3", Name: "c" }],
        total: 3,
        limit: 50,
        offset: 50,
      });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toHaveLength(2);
    expect(result.current.hasMore).toBe(true);
    expect(fetchFn).toHaveBeenCalledWith(0, expect.any(AbortSignal));

    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.loadingMore).toBe(false));
    expect(fetchFn).toHaveBeenCalledWith(50, expect.any(AbortSignal));

    expect(result.current.data).toHaveLength(3);
    expect(result.current.data).toEqual([
      { ID: "1", Name: "a" },
      { ID: "2", Name: "b" },
      { ID: "3", Name: "c" },
    ]);
    expect(result.current.hasMore).toBe(false);
  });

  it("resets pages on fetchFn change", async () => {
    const fetchFn1 = vi.fn().mockResolvedValue({
      items: [{ ID: "1", Name: "first" }],
      total: 1,
      limit: 50,
      offset: 0,
    });
    const fetchFn2 = vi.fn().mockResolvedValue({
      items: [{ ID: "2", Name: "second" }],
      total: 1,
      limit: 50,
      offset: 0,
    });

    const { result, rerender } = renderHook(
      ({
        fn,
      }: {
        fn: (
          offset: number,
        ) => Promise<{ items: Item[]; total: number; limit: number; offset: number }>;
      }) => useSwarmResource(fn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
        initialProps: { fn: fetchFn1 },
      },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual([{ ID: "1", Name: "first" }]);

    rerender({ fn: fetchFn2 });
    await waitFor(() => expect(result.current.data).toEqual([{ ID: "2", Name: "second" }]));
  });

  it("SSE bumps total for unknown items in paginated mode", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      items: [{ ID: "1", Name: "a" }],
      total: 5,
      limit: 50,
      offset: 0,
    });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      {
        wrapper,
      },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.total).toBe(5);
    expect(result.current.data).toHaveLength(1);

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "99",
        resource: { ID: "99", Name: "new-unknown" },
      }),
    );

    // Total should increase but data should NOT have the new item
    expect(result.current.total).toBe(6);
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data[0].ID).toBe("1");
  });

  it("sync event resets to page 0 and discards subsequent pages", async () => {
    const page0 = [
      { ID: "1", Name: "a" },
      { ID: "2", Name: "b" },
    ];
    const page1 = [{ ID: "3", Name: "c" }];
    const refreshed = [
      { ID: "1", Name: "a-refreshed" },
      { ID: "2", Name: "b-refreshed" },
    ];

    const fetchFn = vi
      .fn()
      .mockResolvedValueOnce({ items: page0, total: 3, limit: 50, offset: 0 })
      .mockResolvedValueOnce({ items: page1, total: 3, limit: 50, offset: 50 })
      .mockResolvedValueOnce({ items: refreshed, total: 2, limit: 50, offset: 0 });

    const { result } = renderHook(
      () => useSwarmResource(fetchFn, "service", ({ ID }: Item) => ID),
      { wrapper },
    );

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toHaveLength(2);

    // Load page 1
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.data).toHaveLength(3));

    // Fire sync event — should reset to page 0 only
    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "sync",
        action: "sync",
        id: "",
      }),
    );

    await waitFor(() => expect(result.current.data).toEqual(refreshed));
    expect(result.current.data).toHaveLength(2);
    expect(result.current.total).toBe(2);
  });
});
