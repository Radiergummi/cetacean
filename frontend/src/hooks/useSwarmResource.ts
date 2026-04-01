import type { PagedResponse } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { useCallback, useEffect, useRef, useState } from "react";

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
  const [serverTotal, setServerTotal] = useState(0);
  const [sseOffset, setSSEOffset] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const getIdRef = useRef(getId);
  getIdRef.current = getId;
  const hasLoadedRef = useRef(false);

  const abortRef = useRef<AbortController | null>(null);

  const load = useCallback(() => {
    abortRef.current?.abort();

    const controller = new AbortController();
    abortRef.current = controller;

    // Only show loading skeleton on initial load, not on search/sort refetches
    if (!hasLoadedRef.current) {
      setLoading(true);
    }

    setError(null);
    fetchFn()
      .then((response) => {
        if (controller.signal.aborted) {
          return;
        }

        setData(response.items);
        setServerTotal(response.total);
        setSSEOffset(0);

        hasLoadedRef.current = true;
      })
      .catch((event) => {
        if (!controller.signal.aborted) {
          setError(event);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });
  }, [fetchFn]);

  useEffect(() => {
    load();
    return () => abortRef.current?.abort();
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

      const previous = dataRef.current;

      if (event.action === "remove") {
        const next = previous.filter((item) => getIdRef.current(item) !== event.id);

        if (next.length < previous.length) {
          setData(next);
          setSSEOffset((offset) => offset - 1);
        }
      } else if (event.resource) {
        const resource = event.resource as T;
        const index = previous.findIndex((item) => getIdRef.current(item) === event.id);

        if (index >= 0) {
          const next = [...previous];
          next[index] = resource;
          setData(next);
        } else {
          setData([...previous, resource]);
          setSSEOffset((offset) => offset + 1);
        }
      } else if (event.action !== "remove") {
        // Replayed event without resource payload — refetch to pick up changes
        loadRef.current();
      }
    }, []),
  );

  const total = serverTotal + sseOffset;

  return { data, total, loading, error, retry: load };
}
