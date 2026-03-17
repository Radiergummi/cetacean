import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { useMatchesBreakpoint } from "./useMatchesBreakpoint";

let listeners: Array<(e: { matches: boolean }) => void> = [];
let currentMatches = false;

beforeEach(() => {
  listeners = [];
  currentMatches = false;
  vi.stubGlobal("matchMedia", vi.fn((query: string) => ({
    matches: currentMatches,
    media: query,
    addEventListener: (_: string, cb: (e: { matches: boolean }) => void) => {
      listeners.push(cb);
    },
    removeEventListener: (_: string, cb: (e: { matches: boolean }) => void) => {
      listeners = listeners.filter((l) => l !== cb);
    },
  })));
});

afterEach(() => vi.restoreAllMocks());

describe("useMatchesBreakpoint", () => {
  it('constructs max-width query for "below"', () => {
    renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(matchMedia).toHaveBeenCalledWith("(max-width: 767px)");
  });

  it('constructs min-width query for "above"', () => {
    renderHook(() => useMatchesBreakpoint("md", "above"));
    expect(matchMedia).toHaveBeenCalledWith("(min-width: 768px)");
  });

  it("returns initial match state", () => {
    currentMatches = true;
    const { result } = renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(result.current).toBe(true);
  });

  it("updates when media query changes", () => {
    const { result } = renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(result.current).toBe(false);
    act(() => {
      for (const cb of listeners) cb({ matches: true });
    });
    expect(result.current).toBe(true);
  });

  it("cleans up listener on unmount", () => {
    const { unmount } = renderHook(() => useMatchesBreakpoint("md", "below"));
    expect(listeners).toHaveLength(1);
    unmount();
    expect(listeners).toHaveLength(0);
  });

  it("supports all Tailwind breakpoints", () => {
    renderHook(() => useMatchesBreakpoint("sm", "above"));
    expect(matchMedia).toHaveBeenCalledWith("(min-width: 640px)");

    renderHook(() => useMatchesBreakpoint("lg", "below"));
    expect(matchMedia).toHaveBeenCalledWith("(max-width: 1023px)");

    renderHook(() => useMatchesBreakpoint("xl", "above"));
    expect(matchMedia).toHaveBeenCalledWith("(min-width: 1280px)");

    renderHook(() => useMatchesBreakpoint("2xl", "below"));
    expect(matchMedia).toHaveBeenCalledWith("(max-width: 1535px)");
  });
});
