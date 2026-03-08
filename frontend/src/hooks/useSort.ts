import { useState, useMemo } from "react";
import { useSearchParams } from "react-router-dom";

export type SortDir = "asc" | "desc";

type Accessor<T> = (item: T) => string | number | undefined;

export function useSort<T>(
  items: T[],
  accessors: Record<string, Accessor<T>>,
  defaultKey?: string,
  defaultDir: SortDir = "asc",
) {
  const [params, setParams] = useSearchParams();
  const initialKey = params.get("sort") ?? defaultKey;
  const initialDir = (
    params.get("dir") === "desc" ? "desc" : params.get("dir") === "asc" ? "asc" : defaultDir
  ) as SortDir;

  const [sortKey, setSortKey] = useState<string | undefined>(initialKey);
  const [sortDir, setSortDir] = useState<SortDir>(initialDir);

  const toggle = (key: string) => {
    let newKey: string;
    let newDir: SortDir;
    if (sortKey === key) {
      newDir = sortDir === "asc" ? "desc" : "asc";
      newKey = key;
    } else {
      newKey = key;
      newDir = "asc";
    }
    setSortKey(newKey);
    setSortDir(newDir);
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (newKey === defaultKey && newDir === defaultDir) {
          next.delete("sort");
          next.delete("dir");
        } else {
          next.set("sort", newKey);
          next.set("dir", newDir);
        }
        return next;
      },
      { replace: true },
    );
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

/** Sort state only — no client-side sorting. For use with server-side sort. */
export function useSortParams(defaultKey?: string, defaultDir: SortDir = "asc") {
  const [params, setParams] = useSearchParams();
  const initialKey = params.get("sort") ?? defaultKey;
  const initialDir = (
    params.get("dir") === "desc" ? "desc" : params.get("dir") === "asc" ? "asc" : defaultDir
  ) as SortDir;

  const [sortKey, setSortKey] = useState<string | undefined>(initialKey);
  const [sortDir, setSortDir] = useState<SortDir>(initialDir);

  const toggle = (key: string) => {
    let newKey: string;
    let newDir: SortDir;
    if (sortKey === key) {
      newDir = sortDir === "asc" ? "desc" : "asc";
      newKey = key;
    } else {
      newKey = key;
      newDir = "asc";
    }
    setSortKey(newKey);
    setSortDir(newDir);
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (newKey === defaultKey && newDir === defaultDir) {
          next.delete("sort");
          next.delete("dir");
        } else {
          next.set("sort", newKey);
          next.set("dir", newDir);
        }
        return next;
      },
      { replace: true },
    );
  };

  return { sortKey, sortDir, toggle };
}
