import { useState } from "react";

export type ViewMode = "table" | "grid";

export function useViewMode(
  key: string,
  defaultMode: ViewMode = "table",
): [ViewMode, (m: ViewMode) => void] {
  const [mode, setMode] = useState<ViewMode>(() => {
    const stored = localStorage.getItem(`viewMode:${key}`);
    return stored === "table" || stored === "grid" ? stored : defaultMode;
  });

  const set = (m: ViewMode) => {
    setMode(m);
    localStorage.setItem(`viewMode:${key}`, m);
  };

  return [mode, set];
}
