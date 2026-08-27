import type { FetchResult } from "../api/client";
import { api, emptyMethods } from "../api/client";
import { useDebouncedInvalidation } from "./useDebouncedInvalidation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

export interface DetailResourceOptions {
  /** Fetch history for this resource (default: true). */
  history?: boolean;
  /** Additional React Query keys to invalidate on SSE events. */
  extraQueryKeys?: readonly (readonly unknown[])[];
}

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  ssePath: string,
  options?: DetailResourceOptions,
) {
  const queryClient = useQueryClient();
  const fetchHistory = options?.history !== false;

  const resourceQuery = useQuery({
    queryKey: ["detail", ssePath],
    queryFn: ({ signal }) => fetchFn(key!, signal),
    enabled: !!key,
  });

  const historyQuery = useQuery({
    queryKey: ["detail-history", ssePath],
    queryFn: ({ signal }) => api.history({ resourceId: key!, limit: 10 }, signal),
    enabled: !!key && fetchHistory,
  });

  const invalidationKeys: (readonly unknown[])[] = [["detail", ssePath]];

  if (fetchHistory) {
    invalidationKeys.push(["detail-history", ssePath]);
  }

  if (options?.extraQueryKeys) {
    invalidationKeys.push(...options.extraQueryKeys);
  }

  useDebouncedInvalidation(ssePath, invalidationKeys);

  const data = resourceQuery.data?.data ?? null;
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
    void queryClient.invalidateQueries({ queryKey: ["detail", ssePath] });

    if (fetchHistory) {
      void queryClient.invalidateQueries({ queryKey: ["detail-history", ssePath] });
    }

    if (options?.extraQueryKeys) {
      for (const queryKey of options.extraQueryKeys) {
        void queryClient.invalidateQueries({ queryKey: [...queryKey] });
      }
    }
  }, [queryClient, ssePath, fetchHistory, options?.extraQueryKeys]);

  return { data, history, error, retry, allowedMethods };
}
