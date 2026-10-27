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

  it("stops polling while the tab is in the background", () => {
    const client = createTestQueryClient();
    const spec = { labelKey: "instance", empty: () => ({}) };

    renderHook(() => useMetricsMap("background", spec, false), {
      wrapper: createWrapper(client, { withRouter: false }),
    });

    const [observer] = client.getQueryCache().find({ queryKey: ["metrics-map", "background"] })
      ?.observers ?? [undefined];

    expect(observer?.options.refetchIntervalInBackground).toBeFalsy();
  });
});
