import { useMatchesBreakpoint } from "./useMatchesBreakpoint";
import { renderHook, act } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

let listeners: Array<(event: { matches: boolean }) => void> = [];
let currentMatches = false;

/**
 * A real MediaQueryList has already updated `matches` by the time it dispatches
 * `change`. Emitting the event without moving the store is a state the browser
 * cannot be in, and the hook reads the store rather than the event.
 */
function emitMatches(matches: boolean) {
  currentMatches = matches;

  for (const listener of listeners) {
    listener({ matches });
  }
}

beforeEach(() => {
  listeners = [];
  currentMatches = false;
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({
      get matches() {
        return currentMatches;
      },
      media: query,
      addEventListener: (_: string, listener: (event: { matches: boolean }) => void) => {
        listeners.push(listener);
      },
      removeEventListener: (_: string, listener: (event: { matches: boolean }) => void) => {
        listeners = listeners.filter((existing) => existing !== listener);
      },
    })),
  );
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
    act(() => emitMatches(true));
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
