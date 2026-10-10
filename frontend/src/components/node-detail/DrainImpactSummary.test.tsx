import { DrainImpactSummary } from "./DrainImpactSummary";
import { api } from "@/api/client";
import type { DrainImpact } from "@/api/types";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const impact: DrainImpact = {
  view: "drain-impact",
  subject: "worker-1",
  nodes: [
    { id: "n2", label: "worker-2", type: "node", state: "ready" },
    { id: "s1", label: "web", type: "service", state: "movable", detail: "1 task(s) here" },
    {
      id: "s2",
      label: "trainer",
      type: "service",
      state: "stranded",
      detail: "node.labels.gpu==true",
    },
    { id: "s3", label: "agent", type: "service", state: "global" },
  ],
  edges: [{ source: "s1", target: "n2" }],
};

function renderSummary(enabled = true) {
  return render(
    <DrainImpactSummary
      nodeId="n1"
      enabled={enabled}
    />,
    { wrapper: createWrapper(createTestQueryClient(), { withRouter: false }) },
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("DrainImpactSummary", () => {
  it("fetches nothing until the confirmation is open", () => {
    const fetch = vi.spyOn(api, "nodeDrainImpact").mockResolvedValue(impact);

    renderSummary(false);

    expect(fetch).not.toHaveBeenCalled();
  });

  it("names the services that cannot move, and why", async () => {
    vi.spyOn(api, "nodeDrainImpact").mockResolvedValue(impact);

    renderSummary();

    await waitFor(() => expect(screen.getByText("trainer")).toBeInTheDocument());
    expect(screen.getByText("node.labels.gpu==true")).toBeInTheDocument();
    expect(screen.getByText(/1 service will move to other nodes/)).toBeInTheDocument();
    expect(screen.getByText(/1 global service stops on this node/)).toBeInTheDocument();
  });

  it("does not promise a reschedule when nothing can move", async () => {
    vi.spyOn(api, "nodeDrainImpact").mockResolvedValue({
      ...impact,
      nodes: impact.nodes.filter(({ state }) => state === "stranded"),
      edges: [],
    });

    renderSummary();

    await waitFor(() => expect(screen.getByText("trainer")).toBeInTheDocument());
    expect(screen.queryByText(/will move to other nodes/)).not.toBeInTheDocument();
  });

  it("says when grants hid nodes from the assessment", async () => {
    vi.spyOn(api, "nodeDrainImpact").mockResolvedValue({ ...impact, note: "2 more are hidden" });

    renderSummary();

    await waitFor(() => expect(screen.getByText("2 more are hidden")).toBeInTheDocument());
  });

  it("falls back to a hedged warning when the assessment fails", async () => {
    vi.spyOn(api, "nodeDrainImpact").mockRejectedValue(new Error("nope"));

    renderSummary();

    await waitFor(() =>
      expect(
        screen.getByText(/could not check where this node's tasks can go/i),
      ).toBeInTheDocument(),
    );
  });
});
