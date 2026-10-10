import { NodeActions } from "./NodeActions";
import { api, ApiError } from "@/api/client";
import type { Node } from "@/api/types";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/client")>()),
  api: { removeNode: vi.fn<() => Promise<unknown>>() },
}));

function downNode(role: string): Node {
  return {
    ID: "node1",
    Spec: { Role: role },
    Description: { Hostname: "worker-1" },
    Status: { State: "down" },
  } as unknown as Node;
}

async function removeAndGetRefused(role: string) {
  const user = userEvent.setup();

  render(
    <MemoryRouter>
      <NodeActions
        node={downNode(role)}
        allowedMethods={new Set(["DELETE"])}
      />
    </MemoryRouter>,
  );

  await user.click(screen.getByRole("button", { name: "Remove" }));
  await user.type(screen.getByPlaceholderText("worker-1"), "worker-1");
  await user.click(screen.getByRole("button", { name: "Remove" }));

  await screen.findByText(/Demote the node if it is a manager/);
}

beforeEach(() => {
  vi.mocked(api.removeNode).mockRejectedValue(
    new ApiError("/api/errors/NOD001", "Node Not Down", 409, "refused"),
  );
});

describe("NodeActions", () => {
  it("offers force removal for a refused worker", async () => {
    await removeAndGetRefused("worker");

    expect(screen.getByRole("button", { name: "Force remove" })).toBeInTheDocument();
  });

  // Swarm refuses to remove a manager until it is demoted, force or not.
  it("does not offer force removal for a refused manager", async () => {
    await removeAndGetRefused("manager");

    expect(screen.queryByRole("button", { name: "Force remove" })).not.toBeInTheDocument();
  });
});
