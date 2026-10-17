import CreateResourceDialog from "./CreateResourceDialog";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { toast } from "sonner";
import { afterEach, describe, expect, it, vi } from "vitest";

describe("CreateResourceDialog", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows a failed create in the dialog until it is closed", async () => {
    const toastError = vi.spyOn(toast, "error");
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
    // Inline beside the form that caused it, and only there.
    expect(toastError).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(await screen.findByRole("button", { name: "Create" }));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("stays open while a create is in flight", async () => {
    let rejectCreate: (error: Error) => void = () => {};
    render(
      <CreateResourceDialog
        resourceType="Config"
        onSubmit={() =>
          new Promise<string>((_, reject) => {
            rejectCreate = reject;
          })
        }
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
    await screen.findByRole("button", { name: "Creating\u2026" });

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });

    expect(screen.getByRole("dialog")).toBeInTheDocument();

    await act(async () => rejectCreate(new Error("name conflicts")));

    expect(await screen.findByRole("alert")).toHaveTextContent("name conflicts");
  });
});
