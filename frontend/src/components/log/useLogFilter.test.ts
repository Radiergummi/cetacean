import type { LogLine } from "./log-utils";
import { useLogFilter } from "./useLogFilter";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

function line(message: string): LogLine {
  return { timestamp: "2026-01-01T00:00:00Z", message, level: "info" } as LogLine;
}

describe("useLogFilter match cursor", () => {
  it("keeps the reader's place as new lines arrive", () => {
    // The cursor used to reset whenever the filtered array changed identity,
    // which a live tail does on every frame — so stepping through matches on a
    // streaming log was pulled back to the first hit before it could be read.
    const { result, rerender } = renderHook(({ lines }) => useLogFilter(lines), {
      initialProps: { lines: [line("one"), line("two"), line("three")] },
    });

    act(() => result.current.setMatchIndex(2));
    expect(result.current.matchIndex).toBe(2);

    rerender({ lines: [line("one"), line("two"), line("three"), line("four")] });

    expect(result.current.matchIndex).toBe(2);
  });

  it("starts again when the search itself changes", () => {
    const { result } = renderHook(() => useLogFilter([line("alpha"), line("beta")]));

    act(() => result.current.setMatchIndex(1));
    act(() => result.current.setSearch("a"));

    expect(result.current.matchIndex).toBe(0);
  });

  it("starts again when the level filter changes", () => {
    const { result } = renderHook(() => useLogFilter([line("alpha"), line("beta")]));

    act(() => result.current.setMatchIndex(1));
    act(() => result.current.setLevelFilter("error"));

    expect(result.current.matchIndex).toBe(0);
  });

  it("pulls a cursor left past the end back onto the last match", () => {
    const { result, rerender } = renderHook(({ lines }) => useLogFilter(lines), {
      initialProps: { lines: [line("one"), line("two"), line("three")] },
    });

    act(() => result.current.setMatchIndex(2));
    rerender({ lines: [line("one")] });

    expect(result.current.matchIndex).toBe(0);
  });
});
