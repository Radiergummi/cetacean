import type { FetchResult } from "../api/client";
import { api, emptyMethods, setsEqual } from "../api/client";
import { useDebouncedInvalidation } from "./useDebouncedInvalidation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useRef } from "react";

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
  // Refetch would cause unnecessary re-renders in consumers.
  const rawMethods = resourceQuery.data?.allowedMethods ?? emptyMethods;
  const methodsRef = useRef<Set<string>>(emptyMethods);

  if (!setsEqual(methodsRef.current, rawMethods)) {
    methodsRef.current = rawMethods;
  }

  const allowedMethods = methodsRef.current;

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
