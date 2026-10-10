import { useEffect } from "react";

/**
 * While `active`, asks before the page unloads, so an unsaved form survives a
 * closed tab or the login redirect that an expired session triggers mid-edit.
 */
export function useLeaveGuard(active: boolean) {
  useEffect(() => {
    if (!active) {
      return;
    }

    function handler(event: BeforeUnloadEvent) {
      event.preventDefault();
    }

    window.addEventListener("beforeunload", handler);

    return () => window.removeEventListener("beforeunload", handler);
  }, [active]);
}
