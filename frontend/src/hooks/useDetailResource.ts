import { api, emptyMethods, setsEqual } from "../api/client";
import type { FetchResult } from "../api/client";
import { useResourceStream } from "./useResourceStream";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

export function useDetailResource<T>(
  key: string | undefined,
  fetchFn: (key: string, signal?: AbortSignal) => Promise<FetchResult<T>>,
  ssePath: string,
) {
  const queryClient = useQueryClient();

  const resourceQuery = useQuery({
    queryKey: ["detail", ssePath],
    queryFn: ({ signal }) => fetchFn(key!, signal),
    enabled: !!key,
    retry: false,
  });

  const historyQuery = useQuery({
    queryKey: ["detail-history", key],
    queryFn: ({ signal }) => api.history({ resourceId: key!, limit: 10 }, signal),
    enabled: !!key,
    retry: false,
  });

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useResourceStream(
    ssePath,
    useCallback(() => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;
        void queryClient.invalidateQueries({ queryKey: ["detail", ssePath] });
        void queryClient.invalidateQueries({ queryKey: ["detail-history", key] });
      }, 500);
    }, [queryClient, ssePath, key]),
  );

  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);

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
    void queryClient.invalidateQueries({ queryKey: ["detail-history", key] });
  }, [queryClient, ssePath, key]);

  return { data, history, error, retry, allowedMethods };
}
