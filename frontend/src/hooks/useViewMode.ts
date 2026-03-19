import { useMatchesBreakpoint } from "./useMatchesBreakpoint";
import { useCallback, useState } from "react";

export type ViewMode = "table" | "grid";

export function useViewMode(
  key: string,
  defaultMode: ViewMode = "table",
): [ViewMode, (mode: ViewMode) => void] {
  const isMobile = useMatchesBreakpoint("md", "below");

  const [mode, setMode] = useState<ViewMode>(() => {
    const stored = localStorage.getItem(`viewMode:${key}`);

    return stored === "table" || stored === "grid" ? stored : defaultMode;
  });

  const set = useCallback(
    (mode: ViewMode) => {
      setMode(mode);
      localStorage.setItem(`viewMode:${key}`, mode);
    },
    [key],
  );

  return [isMobile ? "grid" : mode, set];
}
