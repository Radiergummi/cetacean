import type { FetchResult } from "../api/client";
import { api, emptyMethods } from "../api/client";
import { useDebouncedInvalidation } from "./useDebouncedInvalidation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

export interface DetailResourceOptions<T> {
  /** Where the canonical identifier sits on the fetched resource. */
  idOf: (data: T) => string;
  /** Fetch history for this resource (default: true). */
  history?: boolean | undefined;
  /** Additional React Query keys to invalidate on SSE events. */
  extraQueryKeys?: readonly (readonly unknown[])[] | undefined;
}

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  collection: string,
  options: DetailResourceOptions<T>,
) {
  const queryClient = useQueryClient();
  const { idOf, extraQueryKeys, history: fetchHistory = true } = options;

  // The route parameter keys the queries: it is unique per URL and known on
  // the first render, where the canonical ID is not.
  const routePath = `${collection}/${key}`;

  const resourceQuery = useQuery({
    queryKey: ["detail", routePath],
    queryFn: ({ signal }) => fetchFn(key!, signal),
    enabled: !!key,
  });

  const data = resourceQuery.data?.data ?? null;

  // Everything after the first fetch addresses the resource by its canonical
  // ID. A name reaches the resource itself through a redirect, but history
  // takes its identifier in a query parameter, which no redirect rewrites —
  // so a name-addressed page would show an empty feed.
  const canonicalId = data ? idOf(data) : null;

  // Until there is an ID the stream falls back to the route path: a failed
  // fetch exhausts its retries, and the reconnect's sync is what revives the
  // page. The two spellings agree whenever the URL is already canonical, so
  // the common request never reopens the stream.
  const ssePath = canonicalId ? `${collection}/${canonicalId}` : routePath;

  const historyQuery = useQuery({
    queryKey: ["detail-history", routePath],
    queryFn: ({ signal }) => api.history({ resourceId: canonicalId!, limit: 10 }, signal),
    enabled: !!canonicalId && fetchHistory,
  });

  const invalidationKeys: (readonly unknown[])[] = [["detail", routePath]];

  if (fetchHistory) {
    invalidationKeys.push(["detail-history", routePath]);
  }

  if (extraQueryKeys) {
    invalidationKeys.push(...extraQueryKeys);
  }

  useDebouncedInvalidation(ssePath, invalidationKeys);

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
