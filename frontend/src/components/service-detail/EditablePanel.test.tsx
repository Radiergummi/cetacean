import { EditablePanel } from "./EditablePanel";
import { useSubmitShortcut } from "@/hooks/useSubmitShortcut";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

function unload() {
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);

  return event.defaultPrevented;
}

describe("EditablePanel", () => {
  it("asks before the page unloads only while a form is open", () => {
    render(
      <EditablePanel
        title="Labels"
        display={<p>display</p>}
        edit={<input aria-label="value" />}
        onOpen={() => {}}
        onSave={async () => {}}
        canEdit
      />,
    );

    expect(unload()).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(unload()).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(unload()).toBe(false);
  });

  it("saves on Ctrl/Cmd+Enter from inside the form", () => {
    const onSave = vi.fn<() => Promise<void>>(async () => {});

    function Page() {
      useSubmitShortcut();

      return (
        <EditablePanel
          title="Labels"
          display={<p>display</p>}
          edit={<input aria-label="value" />}
          onOpen={() => {}}
          onSave={onSave}
          canEdit
        />
      );
    }

    render(<Page />);
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.keyDown(screen.getByLabelText("value"), { key: "Enter", metaKey: true });

    expect(onSave).toHaveBeenCalledTimes(1);
  });
});
