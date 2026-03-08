import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { useSearchParam } from "./useSearchParam";

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

describe("useSearchParam", () => {
  it("returns empty string by default", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });
    expect(result.current[0]).toBe("");
  });

  it("updates the param value", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });
    act(() => result.current[1]("hello"));
    expect(result.current[0]).toBe("hello");
  });

  it("clears the param when set to empty", () => {
    const { result } = renderHook(() => useSearchParam("q"), { wrapper });
    act(() => result.current[1]("test"));
    expect(result.current[0]).toBe("test");
    act(() => result.current[1](""));
    expect(result.current[0]).toBe("");
  });
});
