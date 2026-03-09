import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { SSEProvider, useSSEConnection, useSSESubscribe } from "./SSEContext";

// Mock EventSource
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

  simulateOpen() {
    this.onopen?.();
  }

  simulateError() {
    this.onerror?.();
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
  return <SSEProvider>{children}</SSEProvider>;
}

describe("useSSEConnection", () => {
  it("starts connected", () => {
    const { result } = renderHook(() => useSSEConnection(), { wrapper });
    expect(result.current.connected).toBe(true);
    expect(result.current.lastEventAt).toBeNull();
  });

  it("becomes disconnected on error", () => {
    const { result } = renderHook(() => useSSEConnection(), { wrapper });
    act(() => MockEventSource.instance.simulateError());
    expect(result.current.connected).toBe(false);
  });

  it("reconnects on open", () => {
    const { result } = renderHook(() => useSSEConnection(), { wrapper });
    act(() => MockEventSource.instance.simulateError());
    act(() => MockEventSource.instance.simulateOpen());
    expect(result.current.connected).toBe(true);
  });

  it("tracks lastEventAt on events", () => {
    const { result } = renderHook(() => useSSEConnection(), { wrapper });
    const before = Date.now();
    act(() =>
      MockEventSource.instance.simulateEvent("node", {
        type: "node",
        action: "update",
        id: "x",
        resource: {},
      }),
    );
    expect(result.current.lastEventAt).toBeGreaterThanOrEqual(before);
    expect(result.current.lastEventAt).toBeLessThanOrEqual(Date.now());
  });

  it("throws outside provider", () => {
    expect(() => renderHook(() => useSSEConnection())).toThrow("must be used within SSEProvider");
  });
});

describe("useSSESubscribe", () => {
  it("calls listener for matching event type", () => {
    const listener = vi.fn();
    renderHook(() => useSSESubscribe(["node"], listener), { wrapper });

    const event = { type: "node", action: "update", id: "abc", resource: {} };
    act(() => MockEventSource.instance.simulateEvent("node", event));

    expect(listener).toHaveBeenCalledWith(event);
  });

  it("does not call listener for non-matching event type", () => {
    const listener = vi.fn();
    renderHook(() => useSSESubscribe(["node"], listener), { wrapper });

    act(() =>
      MockEventSource.instance.simulateEvent("service", {
        type: "service",
        action: "update",
        id: "x",
      }),
    );

    expect(listener).not.toHaveBeenCalled();
  });

  it("unsubscribes on unmount", () => {
    const listener = vi.fn();
    const { unmount } = renderHook(() => useSSESubscribe(["node"], listener), { wrapper });
    unmount();

    act(() =>
      MockEventSource.instance.simulateEvent("node", {
        type: "node",
        action: "update",
        id: "x",
      }),
    );

    expect(listener).not.toHaveBeenCalled();
  });

  it("closes EventSource on provider unmount", () => {
    const { unmount } = renderHook(() => useSSEConnection(), { wrapper });
    unmount();
    expect(MockEventSource.instance.closed).toBe(true);
  });

  it("throws outside provider", () => {
    expect(() => renderHook(() => useSSESubscribe(["node"], vi.fn()))).toThrow(
      "must be used within SSEProvider",
    );
  });
});
