import { Button } from "./button";
import { submitShortcutLabel } from "@/lib/platform";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

describe("Button", () => {
  it("shows the submit shortcut in front of a submit button's label", () => {
    render(<Button type="submit">Create</Button>);

    const button = screen.getByRole("button", { name: /Create/ });

    expect(button).toHaveTextContent(`${submitShortcutLabel}Create`);
    expect(button).toHaveAttribute("aria-keyshortcuts", "Meta+Enter Control+Enter");
  });

  it("shows it on any button that asks for it", () => {
    render(
      <Button
        type="button"
        submitShortcut
      >
        Save
      </Button>,
    );

    expect(screen.getByRole("button")).toHaveAttribute("data-submit-shortcut");
    expect(screen.getByRole("button")).toHaveTextContent(`${submitShortcutLabel}Save`);
  });

  it("leaves other buttons as they are", () => {
    render(<Button>Cancel</Button>);

    expect(screen.getByRole("button")).toHaveTextContent(/^Cancel$/);
    expect(screen.getByRole("button")).not.toHaveAttribute("aria-keyshortcuts");
  });
});
