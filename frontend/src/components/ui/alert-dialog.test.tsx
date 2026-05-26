import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "./alert-dialog";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

function Harness({ onConfirm }: { onConfirm: () => void }) {
  return (
    <AlertDialog>
      <AlertDialogTrigger>Open</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Restart service?</AlertDialogTitle>
          <AlertDialogDescription>This triggers a rolling restart.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm}>Restart</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

describe("AlertDialog", () => {
  it("invokes onConfirm and closes the dialog when the action is clicked", () => {
    const onConfirm = vi.fn<() => void>();
    render(<Harness onConfirm={onConfirm} />);

    fireEvent.click(screen.getByText("Open"));
    expect(screen.getByText("Restart service?")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Restart" }));

    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(screen.queryByText("Restart service?")).not.toBeInTheDocument();
  });

  it("closes the dialog when Cancel is clicked", () => {
    const onConfirm = vi.fn<() => void>();
    render(<Harness onConfirm={onConfirm} />);

    fireEvent.click(screen.getByText("Open"));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(onConfirm).not.toHaveBeenCalled();
    expect(screen.queryByText("Restart service?")).not.toBeInTheDocument();
  });
});
