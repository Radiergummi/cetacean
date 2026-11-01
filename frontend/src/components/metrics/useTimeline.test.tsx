import { useTimeline } from "./useTimeline";
import type { HistoryEntry } from "@/api/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

const timeline = vi.fn<
  (params: Record<string, string | undefined>) => Promise<{
    entries: HistoryEntry[];
    truncated: boolean;
  }>
>();
const invalidation = vi.fn<(path: string | undefined) => void>();

vi.mock("@/api/client", () => ({
  api: { timeline: (params: Record<string, string | undefined>) => timeline(params) },
}));

vi.mock("@/hooks/useDebouncedInvalidation", () => ({
  useDebouncedInvalidation: (path: string | undefined) => invalidation(path),
}));

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

const change = (type: string): HistoryEntry => ({
  id: 1,
  kind: "change",
  timestamp: "2026-10-30T12:00:00Z",
  type,
  action: "update",
  resourceId: "svc1",
  name: "web",
});

describe("useTimeline", () => {
  it("asks for the custom window and keeps the resource's own changes", async () => {
    timeline.mockResolvedValue({ entries: [change("service"), change("task")], truncated: true });

    const { result } = renderHook(
      () =>
        useTimeline(
          { type: "service", resourceId: "svc1" },
          { range: "1h", from: 1_000, to: 2_000, refreshKey: 0 },
        ),
      { wrapper },
    );

    await waitFor(() => expect(result.current.items).toHaveLength(1));

    expect(timeline).toHaveBeenCalledWith({
      type: "service",
      resourceId: "svc1",
      since: "1970-01-01T00:16:40.000Z",
      until: "1970-01-01T00:33:20.000Z",
    });
    expect(result.current.truncated).toBe(true);
    expect(invalidation).toHaveBeenCalledWith("/services/svc1");
  });

  it("keeps the markers up while a refresh loads", async () => {
    timeline.mockResolvedValueOnce({ entries: [change("service")], truncated: false });

    const { result, rerender } = renderHook(
      ({ refreshKey }) =>
        useTimeline({ type: "service", resourceId: "svc1" }, { range: "1h", refreshKey }),
      { wrapper, initialProps: { refreshKey: 0 } },
    );

    await waitFor(() => expect(result.current.items).toHaveLength(1));

    timeline.mockReturnValueOnce(new Promise(() => {}));
    rerender({ refreshKey: 1 });

    expect(result.current.items).toHaveLength(1);
  });

  it("fetches nothing without a resource", () => {
    timeline.mockClear();

    const { result } = renderHook(() => useTimeline(undefined, { range: "1h", refreshKey: 0 }), {
      wrapper,
    });

    expect(result.current.items).toEqual([]);
    expect(timeline).not.toHaveBeenCalled();
  });
});
