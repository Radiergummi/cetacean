import type { FetchResult } from "../api/client";
import { api } from "../api/client";
import { keepChanges } from "../lib/activity";
import { useDebouncedInvalidation } from "./useDebouncedInvalidation";
import type { SSEEvent } from "./useResourceStream";
import { useStableMethods } from "./useStableMethods";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

/**
 * The singular each collection's history ring records. A wrong type reads as an
 * empty feed rather than an error, so the mapping is stated, not derived.
 */
const historyTypes = {
  "/services": "service",
  "/nodes": "node",
  "/tasks": "task",
  "/configs": "config",
  "/secrets": "secret",
  "/networks": "network",
  "/volumes": "volume",
  "/stacks": "stack",
} as const;

/** How many changes a detail page's Recent Activity shows. */
const recentChanges = 10;

/** The page asked for to find them, gaps included. */
const historyPageSize = 50;

export type DetailCollection = keyof typeof historyTypes;

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

/**
 * The cache key a detail page keeps its resource under, for a page that reads
 * the same resource as a neighbour and should share its request.
 */
export function detailQueryKey(collection: DetailCollection, key: string | undefined) {
  return ["detail", key ? `${collection}/${encodeURIComponent(key)}` : undefined] as const;
}

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  collection: DetailCollection,
  options?: DetailResourceOptions<T> | undefined,
) {
  const queryClient = useQueryClient();
  const { extraQueryKeys, history: fetchHistory = true, onEvent } = options ?? {};

  // The route parameter keys the queries: it is unique per URL and known on
  // the first render, where the canonical ID is not.
  const routePath = key ? `${collection}/${encodeURIComponent(key)}` : undefined;

  const detailKey = useMemo(() => detailQueryKey(collection, key), [collection, key]);
  const historyKey = useMemo(() => ["detail-history", routePath], [routePath]);

  const resourceQuery = useQuery({
    queryKey: detailKey,
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
  // it asks alongside the first fetch rather than behind it.
  const resourceType = historyTypes[collection];

  // Gaps are on every resource's timeline, so a page wide enough for the
  // changes is asked for and cut back to them, keeping the gaps among them.
  const historyQuery = useQuery({
    queryKey: historyKey,
    queryFn: async ({ signal }) =>
      keepChanges(
        await api.history({ resourceId: key!, type: resourceType, limit: historyPageSize }, signal),
        recentChanges,
      ),
    enabled: !!key && fetchHistory,
  });

  const invalidationKeys: (readonly unknown[])[] = [detailKey];

  if (fetchHistory) {
    invalidationKeys.push(historyKey);
  }

  if (extraQueryKeys) {
    invalidationKeys.push(...extraQueryKeys);
  }

  const setData = useCallback(
    (update: (previous: T) => T) =>
      queryClient.setQueryData<FetchResult<T>>(detailKey, (previous) =>
        previous ? { ...previous, data: update(previous.data) } : previous,
      ),
    [queryClient, detailKey],
  );

  const handleEvent = useCallback(
    (event: SSEEvent) => {
      onEvent?.(event, {
        setData,
        refetch: () => {
          void queryClient.invalidateQueries({ queryKey: detailKey });
        },
      });
    },
    [onEvent, queryClient, detailKey, setData],
  );

  useDebouncedInvalidation(ssePath, invalidationKeys, 500, onEvent ? handleEvent : undefined);

  const error = resourceQuery.error ?? null;
  const history = historyQuery.data ?? [];

  const allowedMethods = useStableMethods(resourceQuery.data?.allowedMethods);
  const editable = useStableMethods(resourceQuery.data?.editable);

  const retry = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: detailKey });

    if (fetchHistory) {
      void queryClient.invalidateQueries({ queryKey: historyKey });
    }

    if (extraQueryKeys) {
      for (const queryKey of extraQueryKeys) {
        void queryClient.invalidateQueries({ queryKey: [...queryKey] });
      }
    }
  }, [queryClient, detailKey, historyKey, fetchHistory, extraQueryKeys]);

  return { data, history, error, retry, setData, allowedMethods, editable };
}
