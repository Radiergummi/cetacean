import CreateResourceDialog from "./CreateResourceDialog";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("CreateResourceDialog", () => {
  it("shows a failed create in the dialog until it is closed", async () => {
    render(
      <CreateResourceDialog
        resourceType="Config"
        onSubmit={() => Promise.reject(new Error("name conflicts"))}
        canSubmit
        onReset={() => {}}
        canCreate
      >
        <span />
      </CreateResourceDialog>,
      { wrapper: createWrapper(createTestQueryClient()) },
    );

    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    fireEvent.click(await screen.findByRole("button", { name: "Create" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("name conflicts");

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(await screen.findByRole("button", { name: "Create" }));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
