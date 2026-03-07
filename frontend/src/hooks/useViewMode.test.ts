import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useViewMode } from "./useViewMode";

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
});
