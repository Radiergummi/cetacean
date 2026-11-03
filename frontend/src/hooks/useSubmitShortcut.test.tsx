import { useSubmitShortcut } from "./useSubmitShortcut";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

function Shortcut() {
  useSubmitShortcut();

  return null;
}

describe("useSubmitShortcut", () => {
  it.each([{ metaKey: true }, { ctrlKey: true }])(
    "submits the form it is pressed in (%o)",
    (modifier) => {
      const onSubmit = vi.fn<(event: unknown) => void>((event) =>
        (event as Event).preventDefault(),
      );

      render(
        <>
          <Shortcut />
          <form onSubmit={onSubmit}>
            <textarea aria-label="Note" />
          </form>
        </>,
      );

      fireEvent.keyDown(screen.getByLabelText("Note"), { key: "Enter", ...modifier });

      expect(onSubmit).toHaveBeenCalledTimes(1);
    },
  );

  it("leaves a plain Enter alone", () => {
    const onSubmit = vi.fn<() => void>();

    render(
      <>
        <Shortcut />
        <form onSubmit={onSubmit}>
          <textarea aria-label="Note" />
        </form>
      </>,
    );

    fireEvent.keyDown(screen.getByLabelText("Note"), { key: "Enter" });

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("saves the editor it is pressed in", () => {
    const onSave = vi.fn<() => void>();

    render(
      <>
        <Shortcut />
        <div data-submit-scope>
          <input aria-label="Value" />
          <button
            type="button"
            data-submit-shortcut
            onClick={onSave}
          >
            Save
          </button>
        </div>
      </>,
    );

    fireEvent.keyDown(screen.getByLabelText("Value"), { key: "Enter", metaKey: true });

    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("does not press a disabled Save", () => {
    const onSave = vi.fn<() => void>();

    render(
      <>
        <Shortcut />
        <div data-submit-scope>
          <input aria-label="Value" />
          <button
            type="button"
            data-submit-shortcut
            disabled
            onClick={onSave}
          >
            Save
          </button>
        </div>
      </>,
    );

    fireEvent.keyDown(screen.getByLabelText("Value"), { key: "Enter", metaKey: true });

    expect(onSave).not.toHaveBeenCalled();
  });

  it("saves nothing from outside an editor", () => {
    const onSave = vi.fn<() => void>();

    render(
      <>
        <Shortcut />
        <input aria-label="Search" />
        <div data-submit-scope>
          <button
            type="button"
            data-submit-shortcut
            onClick={onSave}
          >
            Save
          </button>
        </div>
      </>,
    );

    fireEvent.keyDown(screen.getByLabelText("Search"), { key: "Enter", metaKey: true });

    expect(onSave).not.toHaveBeenCalled();
  });
});
