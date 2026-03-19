import { createContext, type ReactNode, useCallback, useContext, useMemo, useState } from "react";

type HighlightState = {
  hoveredId: string | null;
  neighbors: Set<string>;
  setHovered: (id: string | null) => void;
};

const ctx = createContext<HighlightState>({
  hoveredId: null,
  neighbors: new Set(),
  setHovered: () => {},
});

export function HighlightProvider({
  edges,
  children,
}: {
  edges: Array<{ source: string; target: string }>;
  children: ReactNode;
}) {
  const [hoveredId, setHoveredId] = useState<string | null>(null);

  const adjacency = useMemo(() => {
    const map = new Map<string, Set<string>>();
    for (const e of edges) {
      if (!map.has(e.source)) {
        map.set(e.source, new Set());
      }

      if (!map.has(e.target)) {
        map.set(e.target, new Set());
      }

      map.get(e.source)!.add(e.target);
      map.get(e.target)!.add(e.source);
    }

    return map;
  }, [edges]);

  const neighbors = useMemo(
    () => (hoveredId ? (adjacency.get(hoveredId) ?? new Set<string>()) : new Set<string>()),
    [hoveredId, adjacency],
  );

  const setHovered = useCallback((id: string | null) => setHoveredId(id), []);

  const value = useMemo(
    () => ({ hoveredId, neighbors, setHovered }),
    [hoveredId, neighbors, setHovered],
  );

  return <ctx.Provider value={value}>{children}</ctx.Provider>;
}

export function useHighlight() {
  return useContext(ctx);
}
