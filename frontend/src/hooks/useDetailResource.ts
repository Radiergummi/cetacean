import type { FetchResult } from "../api/client";
import { api, emptyMethods } from "../api/client";
import { useDebouncedInvalidation } from "./useDebouncedInvalidation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

export interface DetailResourceOptions {
  /** Fetch history for this resource (default: true). */
  history?: boolean | undefined;
  /** Additional React Query keys to invalidate on SSE events. */
  extraQueryKeys?: readonly (readonly unknown[])[] | undefined;
}

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  collection: string,
  options?: DetailResourceOptions | undefined,
) {
  const queryClient = useQueryClient();
  const { extraQueryKeys, history: fetchHistory = true } = options ?? {};

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
