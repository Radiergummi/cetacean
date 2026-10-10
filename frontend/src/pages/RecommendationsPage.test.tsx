import RecommendationsPage from "./RecommendationsPage";
import { api, headAllowedMethods } from "@/api/client";
import type { Recommendation } from "@/api/types";
import { createWrapper } from "@/test/mocks";
import { QueryClient } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

const drainManager: Recommendation = {
  category: "manager-has-workloads",
  severity: "warning",
  scope: "node",
  targetId: "node1",
  targetName: "manager-1",
  resource: "availability",
  message: "Manager node has active availability (may run workloads)",
  suggested: 0,
  fixAction: "PUT /nodes/{id}/availability",
};

let items: Recommendation[] = [];

vi.mock("@/hooks/useRecommendations", () => ({
  useRecommendations: () => ({ items }),
  invalidateRecommendations: vi.fn<() => void>(),
}));

vi.mock("@/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/client")>();

  return {
    ...actual,
    api: {
      ...actual.api,
      updateNodeAvailability: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
    },
    headAllowedMethods: vi.fn<(path: string) => Promise<Set<string>>>(),
  };
});

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(<RecommendationsPage />, { wrapper: createWrapper(queryClient) });
}

describe("RecommendationsPage", () => {
  beforeEach(() => {
    vi.mocked(headAllowedMethods).mockReset();
    items = [scaleHint];
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

  it("asks before draining a node", async () => {
    items = [drainManager];
    vi.mocked(headAllowedMethods).mockResolvedValue(new Set(["GET", "HEAD", "PUT"]));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: /apply suggested value/i }));

    expect(await screen.findByText("Drain this node?")).toBeInTheDocument();
    expect(api.updateNodeAvailability).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Drain" }));

    await waitFor(() => expect(api.updateNodeAvailability).toHaveBeenCalledWith("node1", "drain"));
  });
});
