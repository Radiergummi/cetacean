import type { ServiceRef } from "../api/types";
import { createTestQueryClient, createWrapper } from "../test/mocks";
import DataResourceDetail from "./DataResourceDetail";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

function renderDetail(services: ServiceRef[]) {
  render(
    <DataResourceDetail
      resourceType="secret"
      listLabel="Secrets"
      listPath="/secrets"
      id="s1"
      name="db_v1"
      labels={{}}
      createdAt="2024-01-01T00:00:00Z"
      updatedAt="2024-01-01T00:00:00Z"
      services={services}
      history={[]}
      allowedMethods={new Set(["DELETE"])}
      editable={new Set()}
      onRemove={vi.fn<() => Promise<void>>()}
      onPatchLabels={vi.fn<() => Promise<Record<string, string>>>()}
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );
}

describe("DataResourceDetail removal", () => {
  it("stays disabled while a service's current spec uses it", () => {
    renderDetail([
      { id: "a", name: "api" },
      { id: "b", name: "billing", rollback: true },
    ]);

    expect(screen.getByRole("button", { name: "Remove" })).toBeDisabled();
  });

  // Docker removes what only a previous spec names; the dialog says who loses
  // their rollback target.
  it("allows removing a version only rollbacks need, and names who loses it", async () => {
    renderDetail([{ id: "b", name: "billing", rollback: true }]);

    const remove = screen.getByRole("button", { name: "Remove" });

    expect(remove).toBeEnabled();

    fireEvent.click(remove);

    expect(
      await screen.findByText("billing can no longer roll back to the spec that uses it."),
    ).toBeInTheDocument();
  });
});
