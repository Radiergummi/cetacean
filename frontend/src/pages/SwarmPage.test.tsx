import SwarmPage from "./SwarmPage";
import { api } from "@/api/client";
import { buildDataset } from "@/demo/dataset";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: {
    swarm: vi.fn<() => Promise<unknown>>(),
    plugins: vi.fn<() => Promise<unknown>>(),
  },
}));

function respondWith(allowedMethods: string[]) {
  vi.mocked(api.swarm).mockResolvedValue({
    data: { swarm: buildDataset().swarm, managerAddr: "10.0.0.1:2377" },
    allowedMethods: new Set(allowedMethods),
  } as never);
}

beforeEach(() => {
  vi.mocked(api.plugins).mockResolvedValue({ data: [] } as never);
});

describe("SwarmPage", () => {
  it("hides the join dialogs when the caller may not POST to the swarm", async () => {
    respondWith(["GET", "HEAD", "PATCH"]);

    render(
      <MemoryRouter>
        <SwarmPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Cluster ID")).toBeInTheDocument();
    expect(screen.queryByText("Join Worker")).not.toBeInTheDocument();
    expect(screen.queryByText("Join Manager")).not.toBeInTheDocument();
  });

  it("offers the join dialogs when the caller may POST to the swarm", async () => {
    respondWith(["GET", "HEAD", "PATCH", "POST"]);

    render(
      <MemoryRouter>
        <SwarmPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Join Worker")).toBeInTheDocument();
    expect(screen.getByText("Join Manager")).toBeInTheDocument();
  });
});
