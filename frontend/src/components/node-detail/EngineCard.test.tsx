import { EngineCard } from "./EngineCard";
import { api } from "@/api/client";
import { createWrapper } from "@/test/mocks";
import { QueryClient } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { dockerLatestVersion: vi.fn<() => Promise<unknown>>() },
}));

describe("EngineCard", () => {
  // An air-gapped install answers 503 every time; asking again only adds
  // outbound checks that cannot succeed.
  it("asks for the latest Docker release once, even when it fails", async () => {
    vi.mocked(api.dockerLatestVersion).mockRejectedValue(new Error("503"));

    const client = new QueryClient({
      defaultOptions: { queries: { retry: 2, retryDelay: 0 } },
    });

    render(<EngineCard version="27.0.0" />, { wrapper: createWrapper(client) });

    expect(screen.getByText("27.0.0")).toBeInTheDocument();
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(api.dockerLatestVersion).toHaveBeenCalledTimes(1);
  });
});
