import { emptyMethods, pageSize, setsEqual } from "../api/client";
import type { FetchResult } from "../api/client";
import type { CollectionResponse } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

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
  fetchFn: (offset: number, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>,
  sseType: string,
  getId: (item: T) => string,
) {
  const [pages, setPages] = useState<Map<number, T[]>>(new Map());
  const [serverTotal, setServerTotal] = useState(0);
  const [sseOffset, setSSEOffset] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [allowedMethods, setAllowedMethods] = useState<Set<string>>(emptyMethods);

  const getIdRef = useRef(getId);
  getIdRef.current = getId;
  const hasLoadedRef = useRef(false);
  const pendingRefetch = useRef(false);
  const loadingMoreRef = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  const nextPageRef = useRef(0);

  const loadPage = useCallback(
    (pageNumber: number) => {
      abortRef.current?.abort();

      const controller = new AbortController();
      abortRef.current = controller;

      const isFirstPage = pageNumber === 0;

      if (isFirstPage && !hasLoadedRef.current) {
        setLoading(true);
      }

      if (!isFirstPage) {
        setLoadingMore(true);
        loadingMoreRef.current = true;
      }

      setError(null);

      fetchFn(pageNumber * pageSize, controller.signal)
        .then(({ data: response, allowedMethods: methods }) => {
          if (controller.signal.aborted) {
            return;
          }

          if (isFirstPage) {
            setPages(new Map([[0, response.items]]));
            nextPageRef.current = 1;
            setAllowedMethods((previous) => (setsEqual(previous, methods) ? previous : methods));
          } else {
            setPages((previous) => {
              const next = new Map(previous);
              next.set(pageNumber, response.items);
              return next;
            });
            nextPageRef.current = pageNumber + 1;
          }

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
            setLoadingMore(false);
            loadingMoreRef.current = false;
          }
        });
    },
    [fetchFn],
  );

  useEffect(() => {
    loadPage(0);
    return () => abortRef.current?.abort();
  }, [loadPage]);

  const loadPageRef = useRef(loadPage);
  loadPageRef.current = loadPage;
  const pagesRef = useRef(pages);
  pagesRef.current = pages;

  useResourceStream(
    ssePathMap[sseType] ?? `/events?types=${sseType}`,
    useCallback((event) => {
      if (event.type === "sync") {
        loadPageRef.current(0);

        return;
      }

      const currentPages = pagesRef.current;

      if (event.action === "remove") {
        for (const [pageNumber, items] of currentPages) {
          const filtered = items.filter((item) => getIdRef.current(item) !== event.id);

          if (filtered.length < items.length) {
            setPages((previous) => {
              const next = new Map(previous);
              next.set(pageNumber, filtered);
              return next;
            });
            break;
          }
        }

        // Always decrement: the item may be in an unloaded page.
        setSSEOffset((offset) => offset - 1);
      } else if (event.resource) {
        const resource = event.resource as T;
        let found = false;

        for (const [pageNumber, items] of currentPages) {
          const index = items.findIndex((item) => getIdRef.current(item) === event.id);

          if (index >= 0) {
            found = true;
            setPages((previous) => {
              const next = new Map(previous);
              const updated = [...(previous.get(pageNumber) ?? [])];
              updated[index] = resource;
              next.set(pageNumber, updated);
              return next;
            });
            break;
          }
        }

        if (!found) {
          setSSEOffset((offset) => offset + 1);
        }
      } else {
        // Replayed event without resource payload — schedule a single refetch.
        // Multiple payload-less events in a batch share one refetch via microtask.
        if (!pendingRefetch.current) {
          pendingRefetch.current = true;
          queueMicrotask(() => {
            pendingRefetch.current = false;
            loadPageRef.current(0);
          });
        }
      }
    }, []),
  );

  const data = useMemo(() => {
    const result: T[] = [];
    const sortedKeys = [...pages.keys()].sort((a, b) => a - b);

    for (const key of sortedKeys) {
      const items = pages.get(key);

      if (items) {
        result.push(...items);
      }
    }

    return result;
  }, [pages]);

  const total = serverTotal + sseOffset;
  const hasMore = data.length < total;

  const loadMore = useCallback(() => {
    if (loadingMoreRef.current) {
      return;
    }

    loadPageRef.current(nextPageRef.current);
  }, []);

  const retry = useCallback(() => {
    loadPageRef.current(0);
  }, []);

  return { data, total, loading, loadingMore, error, retry, hasMore, loadMore, allowedMethods };
}
