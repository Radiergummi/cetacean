import { useInstanceResolver } from "./useInstanceResolver";
import { api } from "@/api/client";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("./useMonitoringStatus", () => ({
  useMonitoringStatus: () => ({}),
  isPrometheusReady: () => true,
}));

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { metricsQuery: vi.fn<() => Promise<unknown>>() },
}));

function renderWithNodes(...nodenames: string[]) {
  vi.mocked(api.metricsQuery).mockResolvedValue({
    data: {
      result: nodenames.map((nodename) => ({
        metric: { nodename, instance: `${nodename}:9100` },
      })),
    },
  } as never);

  return renderHook(() => useInstanceResolver(), {
    wrapper: createWrapper(createTestQueryClient(), { withRouter: false }),
  });
}

describe("useInstanceResolver", () => {
  it("does not hand one node another node's instance because its name is a prefix", async () => {
    const { result } = renderWithNodes("worker-1");

    await waitFor(() => expect(result.current.hasData).toBe(true));

    expect(result.current.resolve("worker-12")).toBeNull();
  });

  it("resolves a fully qualified hostname to the exporter's shorter name", async () => {
    const { result } = renderWithNodes("worker-1.internal");

    await waitFor(() => expect(result.current.hasData).toBe(true));

    expect(result.current.resolve("worker-1.internal.example.com")).toBe("worker-1.internal:9100");
    expect(result.current.resolve("worker-1")).toBeNull();
  });
});
