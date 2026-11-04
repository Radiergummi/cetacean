import { useClusterAnnotatable } from "./useClusterAnnotatable";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

const cluster = vi.fn<() => Promise<{ data: object; annotatable?: boolean }>>();

vi.mock("@/api/client", () => ({
  api: { cluster: () => cluster() },
}));

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

describe("useClusterAnnotatable", () => {
  it("allows nothing until the cluster answers, then what its link says", async () => {
    cluster.mockResolvedValue({ data: {}, annotatable: true });

    const { result } = renderHook(() => useClusterAnnotatable(), { wrapper });

    expect(result.current).toBe(false);
    await waitFor(() => expect(result.current).toBe(true));
  });
});
