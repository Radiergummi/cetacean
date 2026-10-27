import { createTestQueryClient, createWrapper } from "../test/mocks";
import { useMetricsMap } from "./useMetricsMap";
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("useMetricsMap", () => {
  it("returns the same empty map on every render while disabled", () => {
    const spec = { labelKey: "instance", empty: () => ({}) };
    const { result, rerender } = renderHook(() => useMetricsMap("disabled", spec, false), {
      wrapper: createWrapper(createTestQueryClient(), { withRouter: false }),
    });
    const first = result.current;

    rerender();

    expect(result.current).toBe(first);
  });
});
