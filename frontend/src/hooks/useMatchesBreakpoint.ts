import { useCallback, useMemo, useSyncExternalStore } from "react";

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

  // One list per query, not one per call: React reads the snapshot on every
  // render, and `matchMedia` parses the query and registers a live object with
  // the style engine each time it is called.
  const mediaQuery = useMemo(() => matchMedia(query), [query]);

  // The viewport is an external store, so React reads it directly rather than
  // mirroring it into state from an effect. That also closes the gap the effect
  // left: a change landing between the first render and the listener being
  // attached used to go unnoticed until the next one.
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      mediaQuery.addEventListener("change", onStoreChange);

      return () => mediaQuery.removeEventListener("change", onStoreChange);
    },
    [mediaQuery],
  );

  return useSyncExternalStore(
    subscribe,
    () => mediaQuery.matches,
    () => false,
  );
}
