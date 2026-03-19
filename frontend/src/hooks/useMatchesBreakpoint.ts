import { useEffect, useState } from "react";

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

  const [matches, setMatches] = useState(() => matchMedia(query).matches);

  useEffect(() => {
    const mediaQuery = matchMedia(query);

    setMatches(mediaQuery.matches);

    const handler = (event: { matches: boolean }) => setMatches(event.matches);

    mediaQuery.addEventListener("change", handler);

    return () => mediaQuery.removeEventListener("change", handler);
  }, [query]);

  return matches;
}
