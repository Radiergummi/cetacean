import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";

export type SortDir = "asc" | "desc";

type Accessor<T> = (item: T) => string | number | undefined;

export function useSort<T>(
  items: T[],
  accessors: Record<string, Accessor<T>>,
  defaultKey?: string,
  defaultDirection: SortDir = "asc",
) {
  const { sortKey, sortDir, toggle } = useSortParams(defaultKey, defaultDirection);

  const sorted = useMemo(() => {
    if (!sortKey || !accessors[sortKey]) {
      return items;
    }

    const get = accessors[sortKey];

    return [...items].sort((a, b) => {
      const av = get(a);
      const bv = get(b);
      let tieBreaker: number;

      if (typeof av === "number" && typeof bv === "number") {
        tieBreaker = av - bv;
      } else {
        tieBreaker = String(av ?? "").localeCompare(String(bv ?? ""));
      }

      return sortDir === "asc" ? tieBreaker : -tieBreaker;
    });
  }, [items, sortKey, sortDir, accessors]);

  return { sorted, sortKey, sortDir, toggle };
}

/** Sort state only — no client-side sorting. For use with server-side sort. */
export function useSortParams(defaultKey?: string, defaultDir: SortDir = "asc") {
  const [params, setParams] = useSearchParams();
  const sortKey = params.get("sort") ?? defaultKey;
  const sortDir = (
    params.get("dir") === "desc" ? "desc" : params.get("dir") === "asc" ? "asc" : defaultDir
  ) as SortDir;

  const toggle = useCallback(
    (key: string) => {
      const newDir = sortKey === key ? (sortDir === "asc" ? "desc" : "asc") : "asc";

      setParams(
        (previous) => {
          const next = new URLSearchParams(previous);

          if (key === defaultKey && newDir === defaultDir) {
            next.delete("sort");
            next.delete("dir");
          } else {
            next.set("sort", key);
            next.set("dir", newDir);
          }
          return next;
        },
        { replace: true },
      );
    },
    [sortKey, sortDir, defaultKey, defaultDir, setParams],
  );

  return { sortKey, sortDir, toggle };
}
