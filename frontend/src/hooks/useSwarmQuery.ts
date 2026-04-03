import { emptyMethods, setsEqual } from "../api/client";
import type { FetchResult } from "../api/client";
import type { CollectionResponse } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import type { InfiniteData } from "@tanstack/react-query";
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

type PageData<T> = CollectionResponse<T>;
type InfinitePages<T> = InfiniteData<PageData<T>, number>;

export function useSwarmQuery<T>(
  queryKey: readonly unknown[],
  fetchFn: (offset: number, signal: AbortSignal) => Promise<FetchResult<CollectionResponse<T>>>,
  sseType: string,
  getId: (item: T) => string,
) {
  const queryClient = useQueryClient();
  const getIdRef = useRef(getId);
  getIdRef.current = getId;
  const queryKeyRef = useRef(queryKey);
  queryKeyRef.current = queryKey;
  const [allowedMethods, setAllowedMethods] = useState<Set<string>>(emptyMethods);

  const query = useInfiniteQuery({
    queryKey: [...queryKey],
    queryFn: async ({ pageParam, signal }) => {
      const result = await fetchFn(pageParam, signal);

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

  const data = query.data?.pages.flatMap((page) => page.items) ?? [];

  const lastPage = query.data?.pages[query.data.pages.length - 1];
  const total = lastPage?.total ?? 0;

  const ssePath = ssePathMap[sseType] ?? `/events?types=${sseType}`;

  useResourceStream(
    ssePath,
    useCallback(
      (event) => {
        const key = [...queryKeyRef.current];

        if (event.type === "sync") {
          queryClient.invalidateQueries({ queryKey: key });

          return;
        }

        const currentData = queryClient.getQueryData<InfinitePages<T>>(key);

        if (!currentData?.pages) {
          return;
        }

        if (event.action === "remove") {
          queryClient.setQueryData<InfinitePages<T>>(key, (old) => {
            if (!old) {
              return old;
            }

            let removed = false;
            const pages = old.pages.map((page) => {
              const filtered = page.items.filter(
                (item) => getIdRef.current(item) !== event.id,
              );

              if (filtered.length < page.items.length) {
                removed = true;
                return { ...page, items: filtered };
              }

              return page;
            });

            if (!removed) {
              return old;
            }

            // Decrement total on ALL pages so getNextPageParam
            // and the displayed count stay consistent.
            return {
              ...old,
              pages: pages.map((page) => ({ ...page, total: page.total - 1 })),
            };
          });
        } else if (event.resource) {
          const resource = event.resource as T;
          let found = false;

          for (const page of currentData.pages) {
            if (page.items.some((item) => getIdRef.current(item) === event.id)) {
              found = true;
              break;
            }
          }

          if (found) {
            queryClient.setQueryData<InfinitePages<T>>(key, (old) => {
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
            queryClient.setQueryData<InfinitePages<T>>(key, (old) => {
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
          queryClient.invalidateQueries({ queryKey: key });
        }
      },
      [queryClient],
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
  }, [query.isFetchingNextPage, query.hasNextPage, query.fetchNextPage]);

  const retry = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: [...queryKey] });
  }, [queryClient, queryKey]);

  return { data, total, loading, loadingMore, error, retry, hasMore, loadMore, allowedMethods };
}
