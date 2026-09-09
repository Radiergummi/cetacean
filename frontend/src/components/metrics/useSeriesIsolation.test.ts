import { useSeriesIsolation } from "./useSeriesIsolation";
import type { ParsedMetrics } from "@/lib/metricsParser.ts";
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

function parsed(labels: string[]): ParsedMetrics {
  return {
    labels: [],
    timestamps: [],
    series: labels.map((label) => ({ label, color: "", data: [] })),
  };
}

describe("useSeriesIsolation", () => {
  describe("uncontrolled", () => {
    it("isolates the clicked dataset", () => {
      const { result } = renderHook(() =>
        useSeriesIsolation({ chartId: "chart", data: parsed(["a", "b", "c"]) }),
      );

      expect(result.current.index).toBeNull();

      act(() => result.current.isolate(1));

      expect(result.current.index).toBe(1);
    });

    /**
     * The isolation is held as a label precisely so this needs no telling: a
     * streamed frame that drops a series must not leave the chart isolating
     * whatever has moved into that index.
     */
    it("drops an isolation whose series is no longer plotted", () => {
      const { result, rerender } = renderHook(
        ({ data }) => useSeriesIsolation({ chartId: "chart", data }),
        { initialProps: { data: parsed(["a", "b"]) } },
      );

      act(() => result.current.isolate(1));
      expect(result.current.index).toBe(1);

      rerender({ data: parsed(["a", "c"]) });

      expect(result.current.index).toBeNull();
    });

    it("follows its series when the index it sits at moves", () => {
      const { result, rerender } = renderHook(
        ({ data }) => useSeriesIsolation({ chartId: "chart", data }),
        { initialProps: { data: parsed(["a", "b"]) } },
      );

      act(() => result.current.isolate(1));

      rerender({ data: parsed(["new", "a", "b"]) });

      expect(result.current.index).toBe(2);
    });

    it("clears on isolate(null)", () => {
      const { result } = renderHook(() =>
        useSeriesIsolation({ chartId: "chart", data: parsed(["a", "b"]) }),
      );

      act(() => result.current.isolate(1));
      act(() => result.current.isolate(null));

      expect(result.current.index).toBeNull();
    });
  });

  describe("controlled", () => {
    it("resolves the caller's label to an index", () => {
      const { result } = renderHook(() =>
        useSeriesIsolation({ chartId: "chart", data: parsed(["a", "b"]), isolatedLabel: "b" }),
      );

      expect(result.current.index).toBe(1);
    });

    it("reports no isolation for a label it does not plot", () => {
      const { result } = renderHook(() =>
        useSeriesIsolation({ chartId: "chart", data: parsed(["a", "b"]), isolatedLabel: "gone" }),
      );

      expect(result.current.index).toBeNull();
    });

    it("reports a click as a label and keeps no state of its own", () => {
      const onIsolationChange = vi.fn<(label: string | null) => void>();
      const { result } = renderHook(() =>
        useSeriesIsolation({
          chartId: "chart",
          data: parsed(["a", "b"]),
          isolatedLabel: null,
          onIsolationChange,
        }),
      );

      act(() => result.current.isolate(1));

      expect(onIsolationChange).toHaveBeenCalledWith("b");
      expect(result.current.index).toBeNull();
    });
  });
});
