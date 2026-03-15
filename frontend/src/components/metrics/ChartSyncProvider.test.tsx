import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import type { ReactNode } from "react";
import { ChartSyncProvider, useChartSync } from "./ChartSyncProvider";

function wrapper({ children }: { children: ReactNode }) {
  return <ChartSyncProvider>{children}</ChartSyncProvider>;
}

describe("ChartSyncProvider", () => {
  it("broadcasts timestamp to subscribers", () => {
    const listener = vi.fn();
    const { result } = renderHook(() => useChartSync(), { wrapper });
    act(() => result.current.subscribe("chart1", listener));
    act(() => result.current.publish("chart2", 1710000000));
    expect(listener).toHaveBeenCalledWith(1710000000);
  });

  it("does not echo timestamp back to publisher", () => {
    const listener = vi.fn();
    const { result } = renderHook(() => useChartSync(), { wrapper });
    act(() => result.current.subscribe("chart1", listener));
    act(() => result.current.publish("chart1", 1710000000));
    expect(listener).not.toHaveBeenCalled();
  });

  it("clears all listeners on clear()", () => {
    const listener = vi.fn();
    const { result } = renderHook(() => useChartSync(), { wrapper });
    act(() => result.current.subscribe("chart1", listener));
    act(() => result.current.clear());
    act(() => result.current.publish("chart2", 1710000000));
    expect(listener).not.toHaveBeenCalled();
  });
});
