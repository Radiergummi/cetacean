import { useEffect } from "react";

/**
 * Ctrl/Cmd+Enter submits the form it is pressed in, through the browser's own
 * validation, or presses the shortcut button of the editor marked
 * `data-submit-scope` around it. Anywhere else it does nothing.
 */
export function useSubmitShortcut() {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event.key !== "Enter" ||
        !(event.metaKey || event.ctrlKey) ||
        event.isComposing ||
        event.defaultPrevented ||
        !(event.target instanceof Element)
      ) {
        return;
      }

      const form = event.target.closest("form");

      if (form) {
        event.preventDefault();
        form.requestSubmit();

        return;
      }

      const button = event.target
        .closest("[data-submit-scope]")
        ?.querySelector<HTMLButtonElement>("[data-submit-shortcut]");

      if (button && !button.disabled) {
        event.preventDefault();
        button.click();
      }
    };

    document.addEventListener("keydown", onKeyDown);

    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);
}
