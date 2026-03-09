import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { useSearchParam } from "./useSearchParam";

beforeEach(() => {
  vi.useFakeTimers();
});

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

describe("useSearchParam", () => {
  it("returns empty strings by default", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });
    const [input, debounced] = result.current;
    expect(input).toBe("");
    expect(debounced).toBe("");
  });

  it("updates input immediately but debounces URL param", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });

    act(() => result.current[2]("hello"));

    // Input updates immediately
    expect(result.current[0]).toBe("hello");
    // URL param not yet updated
    expect(result.current[1]).toBe("");

    // After debounce, URL param updates
    act(() => vi.advanceTimersByTime(300));
    expect(result.current[1]).toBe("hello");
  });

  it("clears the param when set to empty", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });

    act(() => result.current[2]("test"));
    act(() => vi.advanceTimersByTime(300));
    expect(result.current[1]).toBe("test");

    act(() => result.current[2](""));
    expect(result.current[0]).toBe("");
    act(() => vi.advanceTimersByTime(300));
    expect(result.current[1]).toBe("");
  });

  it("coalesces rapid changes", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });

    act(() => result.current[2]("a"));
    act(() => result.current[2]("ab"));
    act(() => result.current[2]("abc"));

    // Input reflects latest value
    expect(result.current[0]).toBe("abc");
    // URL not yet updated
    expect(result.current[1]).toBe("");

    act(() => vi.advanceTimersByTime(300));
    // URL jumps straight to final value
    expect(result.current[1]).toBe("abc");
  });
});
