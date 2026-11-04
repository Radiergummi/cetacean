import { useLatestRef } from "./useLatestRef";
import type { SSEEvent, SSEListener } from "./useResourceStream";
import { useResourceStream } from "./useResourceStream";
import { movesResources } from "@/lib/resourceEvents";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

/** The longest a burst of events defers its refetch, timed from its first event. */
const maximumWait = 5000;

type QueryKeys = readonly (readonly unknown[])[];

interface Options {
  /** Invalidated on an event that may have moved a resource, and a sync. */
  resourceKeys?: QueryKeys | undefined;

  /**
   * Invalidated on every event `timelineRelevant` lets through, a gap's and an
   * annotation's too: what lists a timeline.
   */
  timelineKeys?: QueryKeys | undefined;
  delay?: number | undefined;
  onEvent?: SSEListener | undefined;

  /** Narrows which events invalidate; a sync always does. */
  relevant?: ((event: SSEEvent) => boolean) | undefined;

  /**
   * Narrows further which of those invalidate `timelineKeys`; a sync always does.
   */
  timelineRelevant?: ((event: SSEEvent) => boolean) | undefined;
}

/**
 * Subscribes to an SSE path and invalidates query keys once a burst of events
 * goes quiet for `delay`, or `maximumWait` after its first event, so a stream
 * that never goes quiet (a crash loop, a rollout) still refetches.
 */
export function useDebouncedInvalidation(
  ssePath: string | undefined,
  {
    resourceKeys = [],
    timelineKeys = [],
    delay = 500,
    onEvent,
    relevant,
    timelineRelevant,
  }: Options,
) {
  const queryClient = useQueryClient();
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const burstStartRef = useRef<number | null>(null);
  const movedResourceRef = useRef(false);
  const movedTimelineRef = useRef(false);
  const resourceKeysRef = useLatestRef(resourceKeys);
  const timelineKeysRef = useLatestRef(timelineKeys);
  const onEventRef = useLatestRef(onEvent);
  const relevantRef = useLatestRef(relevant);
  const timelineRelevantRef = useLatestRef(timelineRelevant);

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

        const movesResource = movesResources(event);
        const movesTimeline =
          timelineKeysRef.current.length > 0 &&
          (event.type === "sync" ||
            !timelineRelevantRef.current ||
            timelineRelevantRef.current(event));

        if (!movesResource && !movesTimeline) {
          return;
        }

        movedResourceRef.current ||= movesResource;
        movedTimelineRef.current ||= movesTimeline;

        if (debounceRef.current) {
          clearTimeout(debounceRef.current);
        }

        const now = Date.now();

        burstStartRef.current ??= now;

        const wait = Math.min(delay, burstStartRef.current + maximumWait - now);

        debounceRef.current = setTimeout(() => {
          const keys = [
            ...(movedResourceRef.current ? resourceKeysRef.current : []),
            ...(movedTimelineRef.current ? timelineKeysRef.current : []),
          ];

          debounceRef.current = null;
          burstStartRef.current = null;
          movedResourceRef.current = false;
          movedTimelineRef.current = false;

          for (const key of keys) {
            void queryClient.invalidateQueries({ queryKey: [...key] });
          }
        }, wait);
      },
      [
        queryClient,
        delay,
        resourceKeysRef,
        timelineKeysRef,
        onEventRef,
        relevantRef,
        timelineRelevantRef,
      ],
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
