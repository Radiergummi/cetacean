import SwarmPage from "./SwarmPage";
import { api } from "@/api/client";
import { buildDataset } from "@/demo/dataset";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    swarm: vi.fn<() => Promise<unknown>>(),
    plugins: vi.fn<() => Promise<unknown>>(),
  },
}));

function respondWith(editable: string[]) {
  vi.mocked(api.swarm).mockResolvedValue({
    data: { swarm: buildDataset().swarm, managerAddr: "10.0.0.1:2377" },
    allowedMethods: new Set(["GET", "HEAD"]),
    editable: new Set(editable),
  } as never);
}

beforeEach(() => {
  vi.mocked(api.plugins).mockResolvedValue({ data: [], allowedMethods: new Set() } as never);
});

describe("SwarmPage", () => {
  it("hides the join dialogs when the caller may not rotate the join tokens", async () => {
    respondWith(["raft", "dispatcher"]);

    render(<SwarmPage />, { wrapper: createWrapper(createTestQueryClient()) });

    expect(await screen.findByText("Cluster ID")).toBeInTheDocument();
    expect(screen.queryByText("Join Worker")).not.toBeInTheDocument();
    expect(screen.queryByText("Join Manager")).not.toBeInTheDocument();
  });

  it("offers the join dialogs when the caller may rotate the join tokens", async () => {
    respondWith(["raft", "dispatcher", "rotate-token"]);

    render(<SwarmPage />, { wrapper: createWrapper(createTestQueryClient()) });

    expect(await screen.findByText("Join Worker")).toBeInTheDocument();
    expect(screen.getByText("Join Manager")).toBeInTheDocument();
  });
});
