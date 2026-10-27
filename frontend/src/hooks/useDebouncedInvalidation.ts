import { useLatestRef } from "./useLatestRef";
import type { SSEListener } from "./useResourceStream";
import { useResourceStream } from "./useResourceStream";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

/**
 * Subscribes to an SSE path and invalidates the given query keys once per
 * burst of events. The delay is timed from the burst's first event, so a
 * stream that never goes quiet (a crash loop, a rollout) still refetches.
 */
export function useDebouncedInvalidation(
  ssePath: string | undefined,
  queryKeys: readonly (readonly unknown[])[],
  delay = 500,
  onEvent?: SSEListener | undefined,
) {
  const queryClient = useQueryClient();
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const queryKeysRef = useLatestRef(queryKeys);
  const onEventRef = useLatestRef(onEvent);

  useResourceStream(
    ssePath,
    useCallback(
      (event) => {
        // One stream serves both: a second useResourceStream for a caller that
        // wants the events themselves would open a second EventSource.
        onEventRef.current?.(event);

        debounceRef.current ??= setTimeout(() => {
          debounceRef.current = null;

          for (const key of queryKeysRef.current) {
            void queryClient.invalidateQueries({ queryKey: [...key] });
          }
        }, delay);
      },
      [queryClient, delay, queryKeysRef, onEventRef],
    ),
  );

  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);
}
