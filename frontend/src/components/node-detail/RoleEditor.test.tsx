import { RoleEditor } from "./RoleEditor";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", () => ({
  api: { updateNodeRole: vi.fn<() => Promise<unknown>>().mockResolvedValue({}) },
}));

import { api } from "@/api/client";
const updateNodeRole = vi.mocked(api.updateNodeRole);

function demote(managerCount: number) {
  render(
    <RoleEditor
      nodeId="n1"
      currentRole="manager"
      isLeader={false}
      managerCount={managerCount}
      canEdit
    />,
  );
  fireEvent.click(screen.getByTitle("Edit role"));
  fireEvent.click(screen.getByText("Worker"));
  fireEvent.click(screen.getByRole("button", { name: "Apply" }));
}

describe("RoleEditor", () => {
  beforeEach(() => updateNodeRole.mockClear());

  it("asks for confirmation before a demotion that leaves the bare quorum", async () => {
    demote(3);

    expect(await screen.findByText("Demote this manager?")).toBeInTheDocument();
    expect(updateNodeRole).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Demote" }));
    await waitFor(() => expect(updateNodeRole).toHaveBeenCalledWith("n1", "worker"));
  });

  it("does not demote when the confirmation is cancelled", async () => {
    demote(3);

    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText("Demote this manager?")).not.toBeInTheDocument());
    expect(updateNodeRole).not.toHaveBeenCalled();
  });

  it("demotes without a dialog when quorum has room to spare", async () => {
    demote(5);

    await waitFor(() => expect(updateNodeRole).toHaveBeenCalledWith("n1", "worker"));
    expect(screen.queryByText("Demote this manager?")).not.toBeInTheDocument();
  });
});
