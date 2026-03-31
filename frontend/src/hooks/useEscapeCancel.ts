import { useEffect, useRef } from "react";

/**
 * When `active` is true, intercepts the Escape key (capture phase) and
 * calls `onCancel` instead of letting the global hotkey handler navigate away.
 */
export function useEscapeCancel(active: boolean, onCancel: () => void) {
  const callbackRef = useRef(onCancel);
  callbackRef.current = onCancel;

  useEffect(() => {
    if (!active) {
      return;
    }

    function handler(event: KeyboardEvent) {
      if (event.key === "Escape") {
        // Let open popovers (combobox dropdowns, etc.) handle Escape themselves.
        const target = event.target as Element | null;

        if (target?.closest("[data-slot='popover-content']")) {
          return;
        }

        event.stopPropagation();
        event.preventDefault();
        callbackRef.current();
      }
    }

    document.addEventListener("keydown", handler, true);

    return () => document.removeEventListener("keydown", handler, true);
  }, [active]);
}
