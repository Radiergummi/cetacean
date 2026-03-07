import { useState, useMemo } from "react";

export type SortDir = "asc" | "desc";

type Accessor<T> = (item: T) => string | number | undefined;

export function useSort<T>(
  items: T[],
  accessors: Record<string, Accessor<T>>,
  defaultKey?: string,
  defaultDir: SortDir = "asc",
) {
  const [sortKey, setSortKey] = useState<string | undefined>(defaultKey);
  const [sortDir, setSortDir] = useState<SortDir>(defaultDir);

  const toggle = (key: string) => {
    if (sortKey === key) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("asc");
    }
  };

  const sorted = useMemo(() => {
    if (!sortKey || !accessors[sortKey]) return items;
    const get = accessors[sortKey];
    return [...items].sort((a, b) => {
      const av = get(a);
      const bv = get(b);
      let cmp: number;
      if (typeof av === "number" && typeof bv === "number") {
        cmp = av - bv;
      } else {
        cmp = String(av ?? "").localeCompare(String(bv ?? ""));
      }
      return sortDir === "asc" ? cmp : -cmp;
    });
  }, [items, sortKey, sortDir, accessors]);

  return { sorted, sortKey, sortDir, toggle };
}
