import type { FetchResult } from "../api/client";
import { api, emptyMethods } from "../api/client";
import { useDebouncedInvalidation } from "./useDebouncedInvalidation";
import type { SSEEvent } from "./useResourceStream";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

export interface DetailResourceOptions<T> {
  /** Fetch history for this resource (default: true). */
  history?: boolean | undefined;
  /** Additional React Query keys to invalidate on SSE events. */
  extraQueryKeys?: readonly (readonly unknown[])[] | undefined;

  /**
   * Called for each event on this resource's stream, ahead of the debounced
   * invalidation, for a page that has to act on the event itself. `setData`
   * writes the resource straight into the cache, which is what an event
   * carrying its payload is for.
   */
  onEvent?: ((event: SSEEvent, actions: DetailResourceActions<T>) => void) | undefined;
}

export interface DetailResourceActions<T> {
  /** Updates the cached resource in place; the previous value is passed in so
   * an event carrying only part of it does not drop the rest. */
  setData: (update: (previous: T) => T) => void;
  refetch: () => void;
}

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  collection: string,
  options?: DetailResourceOptions<T> | undefined,
) {
  const queryClient = useQueryClient();
  const { extraQueryKeys, history: fetchHistory = true, onEvent } = options ?? {};

  // The route parameter keys the queries: it is unique per URL and known on
  // the first render, where the canonical ID is not.
  const routePath = key ? `${collection}/${key}` : undefined;

  const resourceQuery = useQuery({
    queryKey: ["detail", routePath],
    queryFn: ({ signal }) => fetchFn(key!, signal),
    enabled: !!key,
  });

  const data = resourceQuery.data?.data ?? null;

  // The stream is the one thing that still needs the canonical spelling: an
  // EventSource would otherwise have to follow the redirect itself. The
  // response names itself in `@id`, so no page has to say where its type keeps
  // an ID. Until it answers the route path stands in — a failed fetch exhausts
  // its retries, and the reconnect's sync is what revives the page.
  const ssePath = resourceQuery.data?.canonicalPath ?? routePath;

  // History resolves its own names now, given the type to resolve against, so
  // it asks alongside the first fetch rather than behind it. The singular is
  // the spelling the ring records, as `resourcePath` also assumes.
  const resourceType = collection.replace(/^\//, "").replace(/s$/, "");

  const historyQuery = useQuery({
    queryKey: ["detail-history", routePath],
    queryFn: ({ signal }) =>
      api.history({ resourceId: key!, type: resourceType, limit: 10 }, signal),
    enabled: !!key && fetchHistory,
  });

  const invalidationKeys: (readonly unknown[])[] = [["detail", routePath]];

  if (fetchHistory) {
    invalidationKeys.push(["detail-history", routePath]);
  }

  if (extraQueryKeys) {
    invalidationKeys.push(...extraQueryKeys);
  }

  const detailKey = ["detail", routePath];

  const handleEvent = useCallback(
    (event: SSEEvent) => {
      onEvent?.(event, {
        setData: (update: (previous: T) => T) =>
          queryClient.setQueryData<FetchResult<T>>(detailKey, (previous) =>
            previous ? { ...previous, data: update(previous.data) } : previous,
          ),
        refetch: () => {
          void queryClient.invalidateQueries({ queryKey: detailKey });
        },
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [onEvent, queryClient, routePath],
  );

  useDebouncedInvalidation(ssePath, invalidationKeys, 500, onEvent ? handleEvent : undefined);

  const error = resourceQuery.error ?? null;
  const history = historyQuery.data ?? [];

  // Stabilize allowedMethods by reference — the Set is recreated on every
  // fetch response, but its contents rarely change. Without this, every SSE
  // refetch would cause unnecessary re-renders in consumers. Keying a memo on
  // the sorted contents gives a stable identity without reading a ref during
  // render.
  const methodsKey = [...(resourceQuery.data?.allowedMethods ?? emptyMethods)].sort().join(",");
  const allowedMethods = useMemo(
    () => new Set(methodsKey === "" ? [] : methodsKey.split(",")),
    [methodsKey],
  );

  const retry = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: ["detail", routePath] });

    if (fetchHistory) {
      void queryClient.invalidateQueries({ queryKey: ["detail-history", routePath] });
    }

    if (extraQueryKeys) {
      for (const queryKey of extraQueryKeys) {
        void queryClient.invalidateQueries({ queryKey: [...queryKey] });
      }
    }
  }, [queryClient, routePath, fetchHistory, extraQueryKeys]);

  return { data, history, error, retry, allowedMethods };
}
