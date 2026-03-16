import { useState, useEffect, useCallback, useRef } from "react";
import { useResourceStream } from "./useResourceStream";
import type { PagedResponse } from "../api/types";

const ssePathMap: Record<string, string> = {
  node: "/nodes",
  service: "/services",
  task: "/tasks",
  config: "/configs",
  secret: "/secrets",
  network: "/networks",
  volume: "/volumes",
  stack: "/stacks",
};

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
  getIdRef.current = getId;
  const hasLoadedRef = useRef(false);

  const load = useCallback(() => {
    // Only show loading skeleton on initial load, not on search/sort refetches
    if (!hasLoadedRef.current) setLoading(true);
    setError(null);
    fetchFn()
      .then((resp) => {
        setData(resp.items);
        setTotal(resp.total);
        hasLoadedRef.current = true;
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, [fetchFn]);

  useEffect(() => {
    load();
  }, [load]);

  const loadRef = useRef(load);
  loadRef.current = load;
  const dataRef = useRef(data);
  dataRef.current = data;

  useResourceStream(
    ssePathMap[sseType] ?? `/events?types=${sseType}`,
    useCallback((event) => {
      if (event.type === "sync") {
        loadRef.current();
        return;
      }
      if (event.action === "remove") {
        setData((prev) => prev.filter((item) => getIdRef.current(item) !== event.id));
        setTotal((prev) => Math.max(0, prev - 1));
      } else if (event.resource) {
        const resource = event.resource as T;
        const isNew =
          dataRef.current.findIndex((item) => getIdRef.current(item) === event.id) < 0;
        setData((prev) => {
          const idx = prev.findIndex((item) => getIdRef.current(item) === event.id);
          if (idx >= 0) {
            const next = [...prev];
            next[idx] = { ...prev[idx], ...resource };
            return next;
          }
          return [...prev, resource];
        });
        if (isNew) setTotal((t) => t + 1);
      }
    }, []),
  );

  return { data, total, loading, error, retry: load };
}
