import { useCallback, useMemo, useSyncExternalStore } from "react";

/**
 * A media query as React reads it: directly, rather than mirrored into state
 * from an effect, which misses a change landing before the listener attaches.
 */
export function useMediaQuery(query: string): boolean {
  // One list per query, not one per call: `matchMedia` parses the query and
  // registers a live object with the style engine each time it is called.
  const mediaQuery = useMemo(() => matchMedia(query), [query]);

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

/** Whether the primary input can hover, which a touch screen cannot. */
export function useCoarsePointer(): boolean {
  return useMediaQuery("(pointer: coarse)");
}
