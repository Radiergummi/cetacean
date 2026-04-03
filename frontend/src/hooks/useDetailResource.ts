import { api, emptyMethods } from "../api/client";
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
    retry: 1,
  });

  const historyQuery = useQuery({
    queryKey: ["history", { resourceId: key, limit: 10 }],
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
        queryClient.invalidateQueries({ queryKey: ["detail", ssePath] });
        queryClient.invalidateQueries({ queryKey: ["history", { resourceId: key }] });
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
  const allowedMethods = resourceQuery.data?.allowedMethods ?? emptyMethods;
  const error = resourceQuery.error ?? null;
  const history = historyQuery.data ?? [];

  const retry = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ["detail", ssePath] });
    queryClient.invalidateQueries({ queryKey: ["history", { resourceId: key }] });
  }, [queryClient, ssePath, key]);

  return { data, history, error, retry, allowedMethods };
}
