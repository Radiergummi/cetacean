import { useMediaQuery } from "./useMediaQuery";

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

  return useMediaQuery(
    direction === "below" ? `(max-width: ${offset - 1}px)` : `(min-width: ${offset}px)`,
  );
}
