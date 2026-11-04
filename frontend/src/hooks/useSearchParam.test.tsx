import { useSearchParam } from "./useSearchParam";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { describe, it, expect, vi, beforeEach } from "vitest";

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

  it("drops a write still waiting when the URL changes elsewhere", () => {
    const { result } = renderHook(() => ({ param: useSearchParam("q"), navigate: useNavigate() }), {
      wrapper,
    });

    act(() => result.current.param[2]("typed"));
    act(() => {
      result.current.navigate("/?q=back");
    });
    act(() => vi.advanceTimersByTime(300));

    expect(result.current.param[1]).toBe("back");
    expect(result.current.param[0]).toBe("back");
  });

  it("keeps typing that its own write lands behind", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });

    act(() => result.current[2]("ab"));
    act(() => {
      vi.advanceTimersByTime(300);
      result.current[2]("abc");
    });

    expect(result.current[1]).toBe("ab");
    expect(result.current[0]).toBe("abc");
  });

  it("updates input immediately but debounces URL param", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });

    act(() => result.current[2]("hello"));

    // Input updates immediately
    expect(result.current[0]).toBe("hello");
    // URL param not yet updated
    expect(result.current[1]).toBe("");

    // After Debounce, URL param updates
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
    // URL isn't yet updated
    expect(result.current[1]).toBe("");

    act(() => vi.advanceTimersByTime(300));
    // URL jumps straight to final value
    expect(result.current[1]).toBe("abc");
  });
});
