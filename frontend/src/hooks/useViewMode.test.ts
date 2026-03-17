import { useViewMode } from "./useViewMode";
import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { useMatchesBreakpoint } from "./useMatchesBreakpoint";

vi.mock("./useMatchesBreakpoint", () => ({
  useMatchesBreakpoint: vi.fn(() => false),
}));

const mockUseMatchesBreakpoint = vi.mocked(useMatchesBreakpoint);

const store = new Map<string, string>();

beforeEach(() => {
  store.clear();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => store.set(key, value),
    removeItem: (key: string) => store.delete(key),
  });
});

describe("useViewMode", () => {
  it("defaults to table", () => {
    const { result } = renderHook(() => useViewMode("test"));
    expect(result.current[0]).toBe("table");
  });

  it("accepts a custom default", () => {
    const { result } = renderHook(() => useViewMode("test", "grid"));
    expect(result.current[0]).toBe("grid");
  });

  it("persists to localStorage", () => {
    const { result } = renderHook(() => useViewMode("test"));
    act(() => result.current[1]("grid"));
    expect(result.current[0]).toBe("grid");
    expect(store.get("viewMode:test")).toBe("grid");
  });

  it("reads from localStorage on init", () => {
    store.set("viewMode:test", "grid");
    const { result } = renderHook(() => useViewMode("test"));
    expect(result.current[0]).toBe("grid");
  });

  it("ignores invalid localStorage values", () => {
    store.set("viewMode:test", "invalid");
    const { result } = renderHook(() => useViewMode("test"));
    expect(result.current[0]).toBe("table");
  });

  describe("mobile", () => {
    beforeEach(() => mockUseMatchesBreakpoint.mockReturnValue(true));
    afterEach(() => mockUseMatchesBreakpoint.mockReturnValue(false));

    it("returns grid on mobile regardless of stored value", () => {
      store.set("viewMode:test", "table");
      const { result } = renderHook(() => useViewMode("test"));
      expect(result.current[0]).toBe("grid");
    });

    it("still persists user choice on mobile", () => {
      const { result } = renderHook(() => useViewMode("test"));
      act(() => result.current[1]("table"));
      expect(store.get("viewMode:test")).toBe("table");
      // But returns grid because mobile override
      expect(result.current[0]).toBe("grid");
    });

    it("returns stored value on desktop", () => {
      mockUseMatchesBreakpoint.mockReturnValue(false);
      store.set("viewMode:test", "table");
      const { result } = renderHook(() => useViewMode("test"));
      expect(result.current[0]).toBe("table");
    });
  });
});
