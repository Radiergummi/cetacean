import ForceRemoveButton from "./ForceRemoveButton";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

describe("ForceRemoveButton", () => {
  it("asks before forcing the remove", async () => {
    const onConfirm = vi.fn<() => void>();
    render(
      <ForceRemoveButton
        name="data"
        disabled={false}
        onConfirm={onConfirm}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Force remove" }));

    expect(await screen.findByText("Force remove data?")).toBeInTheDocument();
    expect(onConfirm).not.toHaveBeenCalled();

    const buttons = screen.getAllByRole("button", { name: "Force remove" });
    fireEvent.click(buttons[buttons.length - 1]!);

    expect(onConfirm).toHaveBeenCalledOnce();
  });
});
