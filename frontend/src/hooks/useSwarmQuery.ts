import type { FetchResult } from "../api/client";
import { emptyMethods, setsEqual } from "../api/client";
import type { CollectionResponse } from "../api/types";
import { useResourceStream } from "./useResourceStream";
import type { InfiniteData } from "@tanstack/react-query";
import { keepPreviousData, useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef } from "react";

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

type PageData<T> = FetchResult<CollectionResponse<T>>;
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

  const query = useInfiniteQuery({
    queryKey: [...queryKey],
    queryFn: async ({ pageParam, signal }) => await fetchFn(pageParam, signal),
    placeholderData: keepPreviousData,
    initialPageParam: 0,
    getNextPageParam: (lastPage) => {
      const page = lastPage.data;
      const nextOffset = page.offset + page.items.length;

      return nextOffset < page.total ? nextOffset : undefined;
    },
  });

  const data = query.data?.pages.flatMap((page) => page.data.items) ?? [];

  const lastPage = query.data?.pages[query.data.pages.length - 1];
  const total = lastPage?.data.total ?? 0;

  const rawMethods = lastPage?.allowedMethods ?? emptyMethods;
  const methodsRef = useRef<Set<string>>(emptyMethods);

  if (!setsEqual(methodsRef.current, rawMethods)) {
    methodsRef.current = rawMethods;
  }

  const allowedMethods = methodsRef.current;

  const ssePath = ssePathMap[sseType] ?? `/events?types=${sseType}`;

  useResourceStream(
    ssePath,
    useCallback(
      (event) => {
        const key = [...queryKeyRef.current];

        if (event.type === "sync") {
          void queryClient.invalidateQueries({ queryKey: key });

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
              const filtered = page.data.items.filter(
                (item) => getIdRef.current(item) !== event.id,
              );

              if (filtered.length < page.data.items.length) {
                removed = true;
                return { ...page, data: { ...page.data, items: filtered } };
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
              pages: pages.map((page) => ({
                ...page,
                data: { ...page.data, total: page.data.total - 1 },
              })),
            };
          });
        } else if (event.resource) {
          const resource = event.resource as T;
          let found = false;

          for (const page of currentData.pages) {
            if (page.data.items.some((item) => getIdRef.current(item) === event.id)) {
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
                  data: {
                    ...page.data,
                    items: page.data.items.map((item) =>
                      getIdRef.current(item) === event.id ? resource : item,
                    ),
                  },
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
                  data: { ...page.data, total: page.data.total + 1 },
                })),
              };
            });
          }
        } else {
          void queryClient.invalidateQueries({ queryKey: key });
        }
      },
      [queryClient],
    ),
  );

  const loading = query.isLoading;
  const loadingMore = query.isFetchingNextPage;
  const error = query.error ?? null;
  const hasMore = query.hasNextPage ?? false;

  const { isFetchingNextPage, hasNextPage, fetchNextPage } = query;
  const loadMore = useCallback(() => {
    if (!isFetchingNextPage && hasNextPage) {
      void fetchNextPage();
    }
  }, [isFetchingNextPage, hasNextPage, fetchNextPage]);

  const retry = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: [...queryKey] });
  }, [queryClient, queryKey]);

  return { data, total, loading, loadingMore, error, retry, hasMore, loadMore, allowedMethods };
}
