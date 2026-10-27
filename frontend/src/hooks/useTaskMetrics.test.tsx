import { useTaskMetricsResult } from "./useTaskMetrics";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { metricsQueryRange: vi.fn<() => Promise<unknown>>() },
}));

beforeEach(() => {
  vi.mocked(api.metricsQueryRange).mockReset();
});

function series(taskId: string, value: string) {
  return {
    status: "success",
    data: {
      resultType: "matrix",
      result: [
        { metric: { container_label_com_docker_swarm_task_id: taskId }, values: [[1, value]] },
      ],
    },
  };
}

describe("useTaskMetricsResult", () => {
  // A task list filtered to failed tasks has nothing running to ask about; the
  // metrics and the error of the running set it showed before are not its own.
  it("forgets the last answer and its error once there is nothing to ask", async () => {
    vi.mocked(api.metricsQueryRange).mockRejectedValue(new Error("Request-URI Too Long"));

    const { result, rerender } = renderHook(({ filters }) => useTaskMetricsResult(filters, true), {
      initialProps: { filters: ['service_id=~"s1"'] as readonly string[] },
      wrapper: createWrapper(createTestQueryClient(), { withRouter: false }),
    });

    await waitFor(() => expect(result.current.error).not.toBeNull());

    rerender({ filters: [] });

    expect(result.current.error).toBeNull();
    expect(result.current.metrics).toEqual({});
  });

  it("asks again only for the filter that changed", async () => {
    vi.mocked(api.metricsQueryRange).mockResolvedValue(series("t1", "5") as never);

    const { rerender } = renderHook(({ filters }) => useTaskMetricsResult(filters, true), {
      initialProps: { filters: ['service_id=~"a"', 'service_id=~"b"'] as readonly string[] },
      wrapper: createWrapper(createTestQueryClient(), { withRouter: false }),
    });

    await waitFor(() => expect(api.metricsQueryRange).toHaveBeenCalledTimes(4));

    rerender({ filters: ['service_id=~"a"', 'service_id=~"c"'] });

    await waitFor(() => expect(api.metricsQueryRange).toHaveBeenCalledTimes(6));

    const askedForA = vi
      .mocked(api.metricsQueryRange)
      .mock.calls.filter(([query]) => (query as string).includes('"a"'));

    expect(askedForA).toHaveLength(2);
  });

  it("merges the series of every filter", async () => {
    vi.mocked(api.metricsQueryRange).mockImplementation(
      async (query: string) => series(query.includes("s1") ? "t1" : "t2", "5") as never,
    );

    const { result } = renderHook(
      () => useTaskMetricsResult(['service_id=~"s1"', 'service_id=~"s2"'], true),
      { wrapper: createWrapper(createTestQueryClient(), { withRouter: false }) },
    );

    await waitFor(() => expect(Object.keys(result.current.metrics).sort()).toEqual(["t1", "t2"]));
  });
});
