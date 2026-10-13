import { EditablePanel } from "./EditablePanel";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

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
});
