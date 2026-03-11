import { useState, useEffect, useCallback, useRef } from "react";
import { useSSE } from "./useSSE";
import type { PagedResponse } from "../api/types";

export function useSwarmResource<T>(
  fetchFn: () => Promise<PagedResponse<T>>,
  sseType: string,
  getId: (item: T) => string,
) {
  const [data, setData] = useState<T[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const getIdRef = useRef(getId);
  const fetchFnRef = useRef(fetchFn);
  getIdRef.current = getId;
  fetchFnRef.current = fetchFn;

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchFnRef
      .current()
      .then((resp) => {
        setData(resp.items);
        setTotal(resp.total);
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [fetchFn]);

  useSSE(
    [sseType],
    useCallback((event) => {
      if (event.action === "remove") {
        setData((prev) => prev.filter((item) => getIdRef.current(item) !== event.id));
      } else if (event.resource) {
        setData((prev) => {
          const resource = event.resource as T;
          const idx = prev.findIndex((item) => getIdRef.current(item) === event.id);
          if (idx >= 0) {
            const next = [...prev];
            next[idx] = resource;
            return next;
          }
          return [...prev, resource];
        });
      }
    }, []),
  );

  return { data, total, loading, error, retry: load };
}
