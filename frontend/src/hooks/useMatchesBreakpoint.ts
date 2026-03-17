import { useEffect, useState } from "react";

const BREAKPOINTS = {
  sm: 640,
  md: 768,
  lg: 1024,
  xl: 1280,
  "2xl": 1536,
} as const;

type Breakpoint = keyof typeof BREAKPOINTS;

export function useMatchesBreakpoint(
  breakpoint: Breakpoint,
  direction: "above" | "below",
): boolean {
  const px = BREAKPOINTS[breakpoint];
  const query = direction === "below" ? `(max-width: ${px - 1}px)` : `(min-width: ${px}px)`;

  const [matches, setMatches] = useState(() => matchMedia(query).matches);

  useEffect(() => {
    const mql = matchMedia(query);
    setMatches(mql.matches);
    const handler = (e: { matches: boolean }) => setMatches(e.matches);
    mql.addEventListener("change", handler);
    return () => mql.removeEventListener("change", handler);
  }, [query]);

  return matches;
}
