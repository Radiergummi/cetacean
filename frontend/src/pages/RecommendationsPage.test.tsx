import RecommendationsPage from "./RecommendationsPage";
import { headAllowedMethods } from "@/api/client";
import type { Recommendation } from "@/api/types";
import { createWrapper } from "@/test/mocks";
import { QueryClient } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const scaleHint = {
  category: "single-replica",
  severity: "info",
  scope: "service",
  targetId: "svc1",
  targetName: "web",
  message: "Service has only 1 replica (no redundancy)",
  fixAction: "PUT /services/{id}/scale",
  suggested: 2,
} as Recommendation;

vi.mock("@/hooks/useRecommendations", () => ({
  useRecommendations: () => ({ items: [scaleHint] }),
  invalidateRecommendations: vi.fn<() => void>(),
}));

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  headAllowedMethods: vi.fn<(path: string) => Promise<Set<string>>>(),
}));

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(<RecommendationsPage />, { wrapper: createWrapper(queryClient) });
}

describe("RecommendationsPage", () => {
  beforeEach(() => {
    vi.mocked(headAllowedMethods).mockReset();
  });

  it("offers a fix the target's Allow admits", async () => {
    vi.mocked(headAllowedMethods).mockResolvedValue(new Set(["GET", "HEAD", "PUT", "POST"]));
    renderPage();

    expect(
      await screen.findByRole("button", { name: /Apply suggested value/ }),
    ).toBeInTheDocument();
    expect(headAllowedMethods).toHaveBeenCalledWith("/services/svc1");
  });

  it("withholds a fix the target's Allow does not admit", async () => {
    vi.mocked(headAllowedMethods).mockResolvedValue(new Set(["GET", "HEAD"]));
    renderPage();

    await vi.waitFor(() => expect(headAllowedMethods).toHaveBeenCalled());
    await screen.findByText(scaleHint.message);
    expect(screen.queryByRole("button", { name: /Apply suggested value/ })).not.toBeInTheDocument();
  });
});
