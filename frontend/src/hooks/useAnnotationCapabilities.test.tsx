import { useAnnotationCapabilities } from "./useAnnotationCapabilities";
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

describe("useAnnotationCapabilities", () => {
  it("allows nothing until the cluster answers, then what its link says", async () => {
    cluster.mockResolvedValue({ data: {}, annotatable: true });

    const { result } = renderHook(() => useAnnotationCapabilities(), { wrapper });

    expect(result.current).toEqual({ clusterWide: false });
    await waitFor(() => expect(result.current).toEqual({ clusterWide: true }));
  });
});
