import type { FetchResult } from "../api/client";
import { emptyMethods } from "../api/client";
import type { CollectionResponse } from "../api/types";
import { useLatestRef } from "./useLatestRef";
import { useResourceStream } from "./useResourceStream";
import type { InfiniteData } from "@tanstack/react-query";
import { keepPreviousData, useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

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
  filtered = false,
) {
  const queryClient = useQueryClient();
  const getIdRef = useLatestRef(getId);
  const queryKeyRef = useLatestRef(queryKey);
  const filteredRef = useLatestRef(filtered);

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

  // Stabilize allowedMethods by reference — the Set is recreated on every
  // fetch response, but its contents rarely change. Without this, every SSE
  // refetch would cause unnecessary re-renders in consumers. Keying a memo on
  // the sorted contents gives a stable identity without reading a ref during
  // render.
  const methodsKey = [...(lastPage?.allowedMethods ?? emptyMethods)].sort().join(",");
  const allowedMethods = useMemo(
    () => new Set(methodsKey === "" ? [] : methodsKey.split(",")),
    [methodsKey],
  );

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

        const held = currentData.pages.some((page) =>
          page.data.items.some((item) => getIdRef.current(item) === event.id),
        );

        if (event.action === "remove") {
          // A filtered collection cannot tell whether an item it does not hold
          // ever belonged to it: the stream carries every resource of this
          // type, not only the ones the search selected. Decrementing on those
          // walks the total below the real count — below zero on a churning
          // cluster — and the load-more sentinel then stops asking for the
          // pages that hold the rows which do match. A stale-high total costs
          // one page fetch instead, which the server answers with a 416
          // carrying the authoritative count.
          if (!held && filteredRef.current) {
            return;
          }

          queryClient.setQueryData<InfinitePages<T>>(key, (old) => {
            if (!old) {
              return old;
            }

            // The item may well live on a page that was never fetched, so a
            // page holding no match is not a no-op: the collection still lost
            // a member. Leaving the total high would keep the table's
            // load-more sentinel chasing an offset past the end of the
            // collection, which the server answers with a 416.
            //
            // Decrement total on ALL pages so getNextPageParam
            // and the displayed count stay consistent.
            return {
              ...old,
              pages: old.pages.map((page) => {
                const items = page.data.items.filter((item) => getIdRef.current(item) !== event.id);

                return {
                  ...page,
                  data: {
                    ...page.data,
                    // Keep the original array when this page held no match, so
                    // only the page that actually lost a row re-renders.
                    items: items.length === page.data.items.length ? page.data.items : items,
                    total: page.data.total - 1,
                  },
                };
              }),
            };
          });
        } else if (event.resource) {
          const resource = event.resource as T;

          if (held) {
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
          } else if (event.action === "create" && !filteredRef.current) {
            // Only a creation changes how many items the collection holds. An
            // update for an item the pages don't hold means it sits on a page
            // that was never fetched — the total already counts it, and
            // counting it again inflates the collection without bound on a
            // churning list. The sentinel then pages past the real end and the
            // server answers 416.
            //
            // A filtered collection sits this out for the reason the removal
            // above does: it cannot tell whether the arrival matches its
            // search, and appending it would show a row the filter excludes.
            queryClient.setQueryData<InfinitePages<T>>(key, (old) => {
              if (!old) {
                return old;
              }

              const loaded = old.pages.reduce((sum, page) => sum + page.data.items.length, 0);
              const lastIndex = old.pages.length - 1;
              const knownTotal = old.pages[lastIndex]?.data.total ?? 0;

              // A list already holding every item it counts has nowhere left
              // to page to, so the new resource belongs on screen now. Raising
              // the total without it would leave the table's load-more
              // sentinel believing a page is still outstanding; it would fetch
              // an offset overlapping what is loaded and render duplicate
              // rows. While pages remain unloaded the count is all we can say:
              // the resource may well sort into one of them.
              const complete = loaded >= knownTotal;

              return {
                ...old,
                pages: old.pages.map((page, index) => ({
                  ...page,
                  data: {
                    ...page.data,
                    total: page.data.total + 1,
                    items:
                      complete && index === lastIndex
                        ? [...page.data.items, resource]
                        : page.data.items,
                  },
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
