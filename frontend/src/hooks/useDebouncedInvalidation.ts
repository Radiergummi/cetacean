import { useLatestRef } from "./useLatestRef";
import type { SSEEvent, SSEListener } from "./useResourceStream";
import { useResourceStream } from "./useResourceStream";
import { isResourceEvent } from "@/lib/resourceEvents";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

/** The longest a burst of events defers its refetch, timed from its first event. */
const maximumWait = 5000;

const noKeys: readonly (readonly unknown[])[] = [];

interface Options {
  /**
   * Invalidated on every event, a gap's and an annotation's too: the queries
   * that list a timeline. The plain keys skip events that move no resource.
   */
  timelineKeys?: readonly (readonly unknown[])[] | undefined;
  delay?: number | undefined;
  onEvent?: SSEListener | undefined;

  /** Narrows which events invalidate; a sync always does. */
  relevant?: ((event: SSEEvent) => boolean) | undefined;
}

/**
 * Subscribes to an SSE path and invalidates the given query keys once a burst
 * of events goes quiet for `delay`, or `maximumWait` after its first event, so
 * a stream that never goes quiet (a crash loop, a rollout) still refetches.
 */
export function useDebouncedInvalidation(
  ssePath: string | undefined,
  queryKeys: readonly (readonly unknown[])[],
  { timelineKeys = noKeys, delay = 500, onEvent, relevant }: Options = {},
) {
  const queryClient = useQueryClient();
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const burstStartRef = useRef<number | null>(null);
  const movedResourceRef = useRef(false);
  const queryKeysRef = useLatestRef(queryKeys);
  const timelineKeysRef = useLatestRef(timelineKeys);
  const onEventRef = useLatestRef(onEvent);
  const relevantRef = useLatestRef(relevant);

  useResourceStream(
    ssePath,
    useCallback(
      (event) => {
        // One stream serves both: a second useResourceStream for a caller that
        // wants the events themselves would open a second EventSource.
        onEventRef.current?.(event);

        if (event.type !== "sync" && relevantRef.current && !relevantRef.current(event)) {
          return;
        }

        const movesResource = event.type === "sync" || isResourceEvent(event);

        if (!movesResource && timelineKeysRef.current.length === 0) {
          return;
        }

        movedResourceRef.current ||= movesResource;

        if (debounceRef.current) {
          clearTimeout(debounceRef.current);
        }

        const now = Date.now();

        burstStartRef.current ??= now;

        const wait = Math.min(delay, burstStartRef.current + maximumWait - now);

        debounceRef.current = setTimeout(() => {
          const keys = movedResourceRef.current
            ? [...queryKeysRef.current, ...timelineKeysRef.current]
            : timelineKeysRef.current;

          debounceRef.current = null;
          burstStartRef.current = null;
          movedResourceRef.current = false;

          for (const key of keys) {
            void queryClient.invalidateQueries({ queryKey: [...key] });
          }
        }, wait);
      },
      [queryClient, delay, queryKeysRef, timelineKeysRef, onEventRef, relevantRef],
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
