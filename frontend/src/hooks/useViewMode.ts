import { useCallback, useState } from "react";
import { useMatchesBreakpoint } from "./useMatchesBreakpoint";

export type ViewMode = "table" | "grid";

export function useViewMode(
  key: string,
  defaultMode: ViewMode = "table",
): [ViewMode, (m: ViewMode) => void] {
  const isMobile = useMatchesBreakpoint("md", "below");

  const [mode, setMode] = useState<ViewMode>(() => {
    const stored = localStorage.getItem(`viewMode:${key}`);
    return stored === "table" || stored === "grid" ? stored : defaultMode;
  });

  const set = useCallback(
    (m: ViewMode) => {
      setMode(m);
      localStorage.setItem(`viewMode:${key}`, m);
    },
    [key],
  );

  return [isMobile ? "grid" : mode, set];
}
