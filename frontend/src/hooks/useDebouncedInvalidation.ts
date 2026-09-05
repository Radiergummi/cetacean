import { useLatestRef } from "./useLatestRef";
import { useResourceStream } from "./useResourceStream";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

/**
 * Subscribes to an SSE path and invalidates the given query keys
 * after a Debounce period. Used by detail pages, cluster overview,
 * and topology to trigger background refetches on data changes.
 */
export function useDebouncedInvalidation(
  ssePath: string,
  queryKeys: readonly (readonly unknown[])[],
  delay = 500,
) {
  const queryClient = useQueryClient();
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const queryKeysRef = useLatestRef(queryKeys);

  useResourceStream(
    ssePath,
    useCallback(() => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }

      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;

        for (const key of queryKeysRef.current) {
          void queryClient.invalidateQueries({ queryKey: [...key] });
        }
      }, delay);
    }, [queryClient, delay, queryKeysRef]),
  );

  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);
}
