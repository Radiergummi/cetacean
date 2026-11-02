import { useAnnotationCapabilities } from "./useAnnotationCapabilities";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

const annotationCapabilities = vi.fn<() => Promise<{ create: boolean; clusterWide: boolean }>>();

vi.mock("@/api/client", () => ({
  api: { annotationCapabilities: () => annotationCapabilities() },
}));

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>;
}

describe("useAnnotationCapabilities", () => {
  it("allows nothing until the server answers", async () => {
    annotationCapabilities.mockResolvedValue({ create: true, clusterWide: true });

    const { result } = renderHook(() => useAnnotationCapabilities(), { wrapper });

    expect(result.current).toEqual({ create: false, clusterWide: false });
    await waitFor(() => expect(result.current).toEqual({ create: true, clusterWide: true }));
  });
});
