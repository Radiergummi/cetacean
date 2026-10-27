import { useLatestRef } from "./useLatestRef";
import type { SSEListener } from "./useResourceStream";
import { useResourceStream } from "./useResourceStream";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

/** The longest a burst of events defers its refetch, timed from its first event. */
const maximumWait = 5000;

/**
 * Subscribes to an SSE path and invalidates the given query keys once a burst
 * of events goes quiet for `delay`, or `maximumWait` after its first event, so
 * a stream that never goes quiet (a crash loop, a rollout) still refetches.
 */
export function useDebouncedInvalidation(
  ssePath: string | undefined,
  queryKeys: readonly (readonly unknown[])[],
  delay = 500,
  onEvent?: SSEListener | undefined,
) {
  const queryClient = useQueryClient();
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const burstStartRef = useRef<number | null>(null);
  const queryKeysRef = useLatestRef(queryKeys);
  const onEventRef = useLatestRef(onEvent);

  useResourceStream(
    ssePath,
    useCallback(
      (event) => {
        // One stream serves both: a second useResourceStream for a caller that
        // wants the events themselves would open a second EventSource.
        onEventRef.current?.(event);

        if (debounceRef.current) {
          clearTimeout(debounceRef.current);
        }

        const now = Date.now();

        burstStartRef.current ??= now;

        const wait = Math.min(delay, burstStartRef.current + maximumWait - now);

        debounceRef.current = setTimeout(() => {
          debounceRef.current = null;
          burstStartRef.current = null;

          for (const key of queryKeysRef.current) {
            void queryClient.invalidateQueries({ queryKey: [...key] });
          }
        }, wait);
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
