import { createTestQueryClient, createWrapper } from "../test/mocks";
import { useMetricsMap } from "./useMetricsMap";
import { api } from "@/api/client";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { metricsQueryRange: vi.fn<() => Promise<unknown>>() },
}));

const rangeSpec = {
  labelKey: "instance",
  empty: () => ({ values: [] as number[] }),
  range: [
    {
      query: "up",
      assign: (metrics: { values: number[] }, values: number[]) => {
        metrics.values = values;
      },
    },
  ],
};

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

  // A list that loads another page changes the key; its cells keep their
  // sparklines until the new answer lands instead of blanking.
  it("keeps the previous answer while a changed query is pending", async () => {
    vi.mocked(api.metricsQueryRange)
      .mockResolvedValueOnce({
        status: "success",
        data: {
          resultType: "matrix",
          result: [{ metric: { instance: "a" }, values: [[1, "5"]] }],
        },
      } as never)
      .mockReturnValue(new Promise(() => {}) as never);

    const { result, rerender } = renderHook(({ key }) => useMetricsMap(key, rangeSpec, true), {
      initialProps: { key: "first" },
      wrapper: createWrapper(createTestQueryClient(), { withRouter: false }),
    });

    await waitFor(() => expect(result.current["a"]?.values).toEqual([5]));

    rerender({ key: "second" });

    expect(result.current["a"]?.values).toEqual([5]);
  });
});
