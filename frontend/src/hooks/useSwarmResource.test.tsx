import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { useSwarmResource } from "./useSwarmResource";

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
    handlers.forEach((h) => h(event));
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
    const fetchFn = vi.fn().mockResolvedValue({ items, total: 1 });

    const { result } = renderHook(() => useSwarmResource(fetchFn, "service", (i: Item) => i.ID), {
      wrapper,
    });

    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual(items);
    expect(result.current.total).toBe(1);
    expect(result.current.error).toBeNull();
  });

  it("handles fetch errors", async () => {
    const fetchFn = vi.fn().mockRejectedValue(new Error("fail"));

    const { result } = renderHook(() => useSwarmResource(fetchFn, "service", (i: Item) => i.ID), {
      wrapper,
    });

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.data).toEqual([]);
  });

  it("updates item on SSE update event", async () => {
    const items: Item[] = [{ ID: "1", Name: "old" }];
    const fetchFn = vi.fn().mockResolvedValue({ items, total: 1 });

    const { result } = renderHook(() => useSwarmResource(fetchFn, "service", (i: Item) => i.ID), {
      wrapper,
    });

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

  it("adds new item on SSE event with unknown id", async () => {
    const fetchFn = vi.fn().mockResolvedValue({ items: [{ ID: "1", Name: "a" }], total: 1 });

    const { result } = renderHook(() => useSwarmResource(fetchFn, "service", (i: Item) => i.ID), {
      wrapper,
    });

    await waitFor(() => expect(result.current.loading).toBe(false));

    const newItem = { ID: "2", Name: "b" };
    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "2",
        resource: newItem,
      }),
    );

    expect(result.current.data).toHaveLength(2);
    expect(result.current.data[1]).toEqual(newItem);
  });

  it("removes item on SSE remove event", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      items: [
        { ID: "1", Name: "a" },
        { ID: "2", Name: "b" },
      ],
      total: 2,
    });

    const { result } = renderHook(() => useSwarmResource(fetchFn, "service", (i: Item) => i.ID), {
      wrapper,
    });

    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "remove",
        id: "1",
      }),
    );

    expect(result.current.data).toEqual([{ ID: "2", Name: "b" }]);
  });

  it("retry re-fetches data", async () => {
    const fetchFn = vi
      .fn()
      .mockRejectedValueOnce(new Error("fail"))
      .mockResolvedValueOnce({ items: [{ ID: "1", Name: "ok" }], total: 1 });

    const { result } = renderHook(() => useSwarmResource(fetchFn, "service", (i: Item) => i.ID), {
      wrapper,
    });

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeInstanceOf(Error);

    act(() => result.current.retry());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toEqual([{ ID: "1", Name: "ok" }]);
    expect(result.current.error).toBeNull();
  });
});
