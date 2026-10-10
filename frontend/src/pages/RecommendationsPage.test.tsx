import type { Recommendation } from "../api/types";
import { createTestQueryClient, createWrapper } from "../test/mocks";
import RecommendationsPage from "./RecommendationsPage";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

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

vi.mock("../hooks/useRecommendations", () => ({
  useRecommendations: () => ({ items: [drainManager] }),
  invalidateRecommendations: vi.fn<() => void>(),
}));

vi.mock("../api/client", () => ({
  api: { updateNodeAvailability: vi.fn<() => Promise<void>>().mockResolvedValue(undefined) },
}));

import { api } from "../api/client";

describe("RecommendationsPage", () => {
  it("asks before draining a node", async () => {
    render(<RecommendationsPage />, { wrapper: createWrapper(createTestQueryClient()) });

    fireEvent.click(screen.getByRole("button", { name: /apply suggested value/i }));

    expect(await screen.findByText("Drain this node?")).toBeInTheDocument();
    expect(api.updateNodeAvailability).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Drain" }));

    await waitFor(() => expect(api.updateNodeAvailability).toHaveBeenCalledWith("node1", "drain"));
  });
});
