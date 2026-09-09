import { useCallback, useSyncExternalStore } from "react";

const breakpoints = {
  sm: 640,
  md: 768,
  lg: 1024,
  xl: 1280,
  "2xl": 1536,
} as const;

type Breakpoint = keyof typeof breakpoints;

export function useMatchesBreakpoint(
  breakpoint: Breakpoint,
  direction: "above" | "below",
): boolean {
  const offset = breakpoints[breakpoint];
  const query = direction === "below" ? `(max-width: ${offset - 1}px)` : `(min-width: ${offset}px)`;

  // The viewport is an external store, so React reads it directly rather than
  // mirroring it into state from an effect. That also closes the gap the effect
  // left: a change landing between the first render and the listener being
  // attached used to go unnoticed until the next one.
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      const mediaQuery = matchMedia(query);

      mediaQuery.addEventListener("change", onStoreChange);

      return () => mediaQuery.removeEventListener("change", onStoreChange);
    },
    [query],
  );

  return useSyncExternalStore(
    subscribe,
    () => matchMedia(query).matches,
    () => false,
  );
}
