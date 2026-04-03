import { emptyMethods, setsEqual } from "../api/client";
import type { FetchResult } from "../api/client";
import type { CollectionResponse } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import { keepPreviousData, useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";

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

export function useSwarmQuery<T>(
  queryKey: readonly unknown[],
  fetchFn: (offset: number, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>,
  sseType: string,
  getId: (item: T) => string,
) {
  const queryClient = useQueryClient();
  const getIdRef = useRef(getId);
  getIdRef.current = getId;
  const [allowedMethods, setAllowedMethods] = useState<Set<string>>(emptyMethods);

  const query = useInfiniteQuery({
    queryKey: [...queryKey],
    queryFn: async ({ pageParam, signal }) => {
      const result = await fetchFn(pageParam, signal);

      // Update allowedMethods from the latest response.
      setAllowedMethods((previous) =>
        setsEqual(previous, result.allowedMethods) ? previous : result.allowedMethods,
      );

      return result.data;
    },
    placeholderData: keepPreviousData,
    initialPageParam: 0,
    getNextPageParam: (lastPage) => {
      const nextOffset = lastPage.offset + lastPage.items.length;
      return nextOffset < lastPage.total ? nextOffset : undefined;
    },
  });

  // Flat data array from all pages.
  const data = query.data?.pages.flatMap((page) => page.items) ?? [];

  // Total from the most recent page (freshest server value).
  const lastPage = query.data?.pages[query.data.pages.length - 1];
  const total = lastPage?.total ?? 0;

  // SSE optimistic cache mutations.
  const ssePath = ssePathMap[sseType] ?? `/events?types=${sseType}`;

  useResourceStream(
    ssePath,
    useCallback(
      (event) => {
        if (event.type === "sync") {
          queryClient.invalidateQueries({ queryKey: [...queryKey] });
          return;
        }

        const currentPages = query.data?.pages;
        if (!currentPages) {
          return;
        }

        if (event.action === "remove") {
          queryClient.setQueryData([...queryKey], (old: typeof query.data) => {
            if (!old) {
              return old;
            }

            return {
              ...old,
              pages: old.pages.map((page) => {
                const filtered = page.items.filter((item) => getIdRef.current(item) !== event.id);

                if (filtered.length === page.items.length) {
                  return page;
                }

                return {
                  ...page,
                  items: filtered,
                  total: page.total - 1,
                };
              }),
            };
          });
        } else if (event.resource) {
          const resource = event.resource as T;
          let found = false;

          // Check if item exists in any loaded page.
          for (const page of currentPages) {
            if (page.items.some((item) => getIdRef.current(item) === event.id)) {
              found = true;
              break;
            }
          }

          if (found) {
            // Update in-place.
            queryClient.setQueryData([...queryKey], (old: typeof query.data) => {
              if (!old) {
                return old;
              }

              return {
                ...old,
                pages: old.pages.map((page) => ({
                  ...page,
                  items: page.items.map((item) =>
                    getIdRef.current(item) === event.id ? resource : item,
                  ),
                })),
              };
            });
          } else {
            // Unknown item: bump total on all pages so hasMore updates.
            queryClient.setQueryData([...queryKey], (old: typeof query.data) => {
              if (!old) {
                return old;
              }

              return {
                ...old,
                pages: old.pages.map((page) => ({
                  ...page,
                  total: page.total + 1,
                })),
              };
            });
          }
        } else {
          // Event without resource payload — invalidate.
          queryClient.invalidateQueries({ queryKey: [...queryKey] });
        }
      },
      // eslint-disable-next-line react-hooks/exhaustive-deps
      [queryClient, query.data?.pages],
    ),
  );

  const loading = query.isLoading;
  const loadingMore = query.isFetchingNextPage;
  const error = query.error ?? null;
  const hasMore = query.hasNextPage ?? false;

  const loadMore = useCallback(() => {
    if (!query.isFetchingNextPage && query.hasNextPage) {
      query.fetchNextPage();
    }
  }, [query]);

  const retry = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: [...queryKey] });
  }, [queryClient, queryKey]);

  return { data, total, loading, loadingMore, error, retry, hasMore, loadMore, allowedMethods };
}
