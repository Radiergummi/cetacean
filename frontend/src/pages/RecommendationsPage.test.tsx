import RecommendationsPage from "./RecommendationsPage";
import { api, headCapabilities } from "@/api/client";
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
    headCapabilities: vi.fn<typeof actual.headCapabilities>(),
  };
});

function capabilities(...editable: string[]) {
  return { methods: new Set(["GET", "HEAD"]), editable: new Set(editable) };
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(<RecommendationsPage />, { wrapper: createWrapper(queryClient) });
}

describe("RecommendationsPage", () => {
  beforeEach(() => {
    vi.mocked(headCapabilities).mockReset();
    items = [scaleHint];
  });

  it("offers a fix the target links as editable", async () => {
    vi.mocked(headCapabilities).mockResolvedValue(capabilities("scale", "restart"));
    renderPage();

    // The page and the Allow probe both resolve late under a loaded full run,
    // which outlasts findByRole's default second.
    expect(
      await screen.findByRole("button", { name: /Apply suggested value/ }, { timeout: 5000 }),
    ).toBeInTheDocument();
    expect(headCapabilities).toHaveBeenCalledWith("/services/svc1");
  });

  it("withholds a fix the target does not link", async () => {
    vi.mocked(headCapabilities).mockResolvedValue(capabilities());
    renderPage();

    await vi.waitFor(() => expect(headCapabilities).toHaveBeenCalled());
    await screen.findByText(scaleHint.message);
    expect(screen.queryByRole("button", { name: /Apply suggested value/ })).not.toBeInTheDocument();
  });

  it("asks before draining a node", async () => {
    items = [drainManager];
    vi.mocked(headCapabilities).mockResolvedValue(capabilities("availability"));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: /apply suggested value/i }));

    expect(await screen.findByText("Drain this node?")).toBeInTheDocument();
    expect(api.updateNodeAvailability).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Drain" }));

    await waitFor(() => expect(api.updateNodeAvailability).toHaveBeenCalledWith("node1", "drain"));
  });
});
