import type { LogLine as ApiLogLine } from "../../api/client";
import { api } from "../../api/client";
import { getErrorInfo } from "../../lib/errors";
import { getErrorMessage } from "../../lib/utils";
import type { LogLine, TimeRange } from "./log-utils";
import { maxLiveLines, toLogLine } from "./log-utils";
import type { LogStreamFailure } from "./logStream";
import { maxReconnectAttempts, streamLogsWithRetry } from "./logStream";
import { useCallback, useEffect, useRef, useState } from "react";

/**
 * The live tail's state, prepared for display. The toolbar renders this
 * directly and never sees the transport's types.
 */
export interface LiveTailStatus {
  state: "streaming" | "retrying" | "stopped";
  attempt: number;
  maxAttempts: number;
  /** Epoch milliseconds of the next attempt; set only while retrying. */
  retryAt?: number | undefined;
  /** What the server said, when it said anything. */
  reason?: string | undefined;
}

/**
 * Prefers the shared error dictionary's wording for a known error code so the
 * log tail says the same thing about a failure as the rest of the dashboard.
 */
function describeFailure({ error }: LogStreamFailure): string | undefined {
  if (!error) {
    return undefined;
  }

  return getErrorInfo(error.code)?.title ?? error.title;
}

interface UseLogDataOptions {
  logId: string;
  isTask: boolean;
  timeRange: TimeRange;
  streamFilter: "all" | "stdout" | "stderr";
}

export function useLogData({ logId, isTask, timeRange, streamFilter }: UseLogDataOptions) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [hasOlderLogs, setHasOlderLogs] = useState(true);
  const [loadingNewer, setLoadingNewer] = useState(false);
  const [hasNewerLogs, setHasNewerLogs] = useState(false);
  const [following, setFollowing] = useState(true);
  const [atTop, setAtTop] = useState(false);
  const [live, setLive] = useState(false);
  const [liveStatus, setLiveStatus] = useState<LiveTailStatus | null>(null);

  const limit = 500;
  const oldestRef = useRef<string | undefined>(undefined);
  const newestRef = useRef<string | undefined>(undefined);
  const containerRef = useRef<HTMLDivElement>(null);
  const fetchAbortRef = useRef<AbortController | null>(null);
  const scrollRafRef = useRef(0);
  // Tracks programmatic scroll-to-bottom so the resulting scroll event
  // doesn't disable follow-mode. Under high log rates the virtualizer
  // measures new rows after we assign scrollTop, leaving us a few pixels
  // above the (new) bottom — without this guard handleScroll would read
  // that gap as user-initiated and turn following off.
  const programmaticScrollRef = useRef(false);

  const streamParam = streamFilter === "all" ? undefined : streamFilter;

  const fetchLogs = useCallback(() => {
    fetchAbortRef.current?.abort();
    setLoading(true);
    setError(null);
    setHasOlderLogs(true);
    setHasNewerLogs(false);
    oldestRef.current = undefined;
    newestRef.current = undefined;

    const controller = new AbortController();
    fetchAbortRef.current = controller;
    let timedOut = false;
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, 15_000);

    const options = {
      limit,
      after: timeRange.since,
      before: timeRange.until,
      stream: streamParam,
      signal: controller.signal,
    };
    const request = isTask ? api.taskLogs(logId, options) : api.serviceLogs(logId, options);

    request
      .then((response) => {
        // A late-arriving response from a previous fetch must not overwrite
        // newer state. Rapid filter / time-range changes used to allow this.
        if (controller.signal.aborted) {
          return;
        }

        const newLines = response.lines?.map(toLogLine) ?? [];
        setLines(newLines);
        oldestRef.current = response.oldest;
        newestRef.current = response.newest;
        setHasOlderLogs(response.hasMore ?? newLines.length >= limit);
        setLoading(false);
      })
      .catch((caught) => {
        if (controller.signal.aborted && !timedOut) {
          // Aborted (e.g., by React StrictMode remount) — don't update state,
          // the next fetch will handle it.
          return;
        }
        if (timedOut) {
          setError("Request timed out");
        } else {
          setError(getErrorMessage(caught, "Failed to load logs"));
        }
        setLoading(false);
      })
      .finally(() => clearTimeout(timeout));
  }, [logId, isTask, limit, timeRange, streamParam]);

  useEffect(() => {
    fetchLogs();
    return () => {
      fetchAbortRef.current?.abort();
    };
  }, [fetchLogs]);

  // Live-streaming via SSE. The transport owns reconnection and backoff; this
  // effect only wires it to React state.
  useEffect(() => {
    if (!live) return;

    const abortController = new AbortController();
    const buffer: ApiLogLine[] = [];
    let animationFrameId = 0;

    const flush = () => {
      animationFrameId = 0;
      if (buffer.length === 0) return;
      const batch = buffer.splice(0);
      setLines((current) => {
        const appended = current.concat(
          batch.map((line, index) => toLogLine(line, current.length + index)),
        );
        if (appended.length <= maxLiveLines) {
          return appended;
        }
        // Re-index after the ring-buffer trim — otherwise the .index field
        // (used for search highlight + jump-to-line) refers to positions
        // that no longer exist in the array.
        return appended.slice(-maxLiveLines).map((line, index) => ({ ...line, index }));
      });
    };

    const urlFor = (cursor: string) => {
      const streamOptions = { after: cursor, stream: streamParam };

      return isTask
        ? api.taskLogsStreamURL(logId, streamOptions)
        : api.serviceLogsStreamURL(logId, streamOptions);
    };

    void streamLogsWithRetry(
      urlFor,
      newestRef.current || new Date().toISOString(),
      {
        onLine: (line) => {
          buffer.push(line);

          if (!animationFrameId) {
            animationFrameId = requestAnimationFrame(flush);
          }
        },
        onStreaming: () => {
          setLiveStatus({ state: "streaming", attempt: 0, maxAttempts: maxReconnectAttempts });
        },
        onRetrying: ({ attempt, maxAttempts, retryAt, failure }) => {
          setLiveStatus({
            state: "retrying",
            attempt,
            maxAttempts,
            retryAt,
            reason: describeFailure(failure),
          });
        },
        onGaveUp: (failure) => {
          setLiveStatus({
            state: "stopped",
            attempt: maxReconnectAttempts,
            maxAttempts: maxReconnectAttempts,
            reason: describeFailure(failure),
          });
          setLive(false);
        },
      },
      abortController.signal,
    );

    return () => {
      abortController.abort();
      cancelAnimationFrame(animationFrameId);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live, logId, isTask, streamParam]);

  // Auto-scroll to the bottom when following and lines change.
  // On the initial mount the virtualizer handles scrolling via initialOffset.
  // This effect covers the non-virtual case and subsequent updates.
  // The rAF is stored in a Ref so React's effect cleanup can't cancel it.
  useEffect(() => {
    if (!following || !containerRef.current) return;
    const node = containerRef.current;
    cancelAnimationFrame(scrollRafRef.current);
    scrollRafRef.current = requestAnimationFrame(() => {
      // Two rAFs: first one lets the virtualizer commit/measure the new
      // rows, second one applies the scroll against the now-final
      // scrollHeight. Without this we land short of the bottom and the
      // resulting scroll event toggles follow-mode off.
      scrollRafRef.current = requestAnimationFrame(() => {
        programmaticScrollRef.current = true;
        node.scrollTop = node.scrollHeight;
        // Guarantee the flag clears even if no scroll event fires
        // (e.g., the assignment was a no-op because we were already at
        // the bottom).
        requestAnimationFrame(() => {
          programmaticScrollRef.current = false;
        });
      });
    });
  }, [lines, following]);

  const loadOlder = useCallback(() => {
    if (loadingOlder || !hasOlderLogs || !oldestRef.current) return;
    setLoadingOlder(true);

    const options = { limit, before: oldestRef.current, stream: streamParam };
    const request = isTask ? api.taskLogs(logId, options) : api.serviceLogs(logId, options);

    request
      .then((response) => {
        const olderLines = response.lines?.map(toLogLine) ?? [];
        if (olderLines.length === 0) {
          setHasOlderLogs(false);
        } else {
          setHasOlderLogs(response.hasMore ?? olderLines.length >= limit);
          oldestRef.current = response.oldest;
          const scrollElement = containerRef.current;
          const previousScrollHeight = scrollElement?.scrollHeight ?? 0;
          setLines((current) =>
            [...olderLines, ...current].map((line, index) => ({ ...line, index })),
          );
          // Suppress the resulting scroll event so handleScroll doesn't flip
          // follow-mode based on the still-growing virtualizer height.
          programmaticScrollRef.current = true;
          // Two rAFs: the first lets React commit and the virtualizer
          // measure the prepended rows; the second adjusts scrollTop
          // against the final scrollHeight. Without this we land at a
          // visibly wrong position when the virtualizer is mid-measure.
          requestAnimationFrame(() => {
            requestAnimationFrame(() => {
              if (scrollElement) {
                scrollElement.scrollTop += scrollElement.scrollHeight - previousScrollHeight;
              }
              requestAnimationFrame(() => {
                programmaticScrollRef.current = false;
              });
            });
          });
        }
        setLoadingOlder(false);
      })
      .catch(() => setLoadingOlder(false));
  }, [loadingOlder, hasOlderLogs, limit, streamParam, isTask, logId]);

  const loadNewer = useCallback(() => {
    if (loadingNewer || !hasNewerLogs || !newestRef.current) return;
    setLoadingNewer(true);

    const options = { limit, after: newestRef.current, stream: streamParam };
    const request = isTask ? api.taskLogs(logId, options) : api.serviceLogs(logId, options);

    request
      .then((response) => {
        const newerLines = response.lines?.map(toLogLine) ?? [];
        if (newerLines.length > 0) {
          newestRef.current = response.newest;
          setLines((current) =>
            [...current, ...newerLines].map((line, index) => ({ ...line, index })),
          );
        } else {
          setHasNewerLogs(false);
        }
        setLoadingNewer(false);
      })
      .catch(() => setLoadingNewer(false));
  }, [loadingNewer, hasNewerLogs, limit, streamParam, isTask, logId]);

  // When not live, check once for newer log availability after the initial load.
  // Uses a generation counter to avoid racing with fetchLogs when streamParam changes.
  const newerCheckGenRef = useRef(0);
  useEffect(() => {
    newerCheckGenRef.current++;
  }, [streamParam]);

  useEffect(() => {
    if (live || loading) return;
    const cursor = newestRef.current;
    if (!cursor) return;
    const gen = newerCheckGenRef.current;
    const options = { limit: 1, after: cursor, stream: streamParam };
    const request = isTask ? api.taskLogs(logId, options) : api.serviceLogs(logId, options);
    request
      .then((response) => {
        // Only apply if streamParam hasn't changed since we started
        if (newerCheckGenRef.current !== gen) return;
        setHasNewerLogs((response.lines?.length ?? 0) > 0);
      })
      .catch(console.warn);
  }, [live, loading, logId, isTask, streamParam]);

  const handleScroll = useCallback(() => {
    const element = containerRef.current;
    if (!element) return;
    // Swallow scroll events that we initiated ourselves. The virtualizer can
    // still be measuring rows when the assignment lands, leaving a gap of a
    // few hundred pixels that would otherwise look like the user scrolled up.
    if (programmaticScrollRef.current) {
      programmaticScrollRef.current = false;
      setAtTop(element.scrollTop < 50);
      return;
    }
    // Generous threshold: row heights vary (multi-line JSON, expanded rows)
    // and a strict bound breaks follow-mode under burst SSE updates.
    const atBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 200;
    setFollowing(atBottom);
    setAtTop(element.scrollTop < 50);
  }, []);

  // Tearing down the effect aborts the stream, so there is nothing to close
  // here beyond flipping the flag.
  const stopLive = useCallback(() => {
    setLive(false);
    setLiveStatus(null);
  }, []);

  const resumeLive = useCallback(() => {
    setFollowing(true);
    setLive(true);
  }, []);

  const toggleLive = useCallback(() => {
    if (live) {
      stopLive();
    } else {
      resumeLive();
    }
  }, [live, stopLive, resumeLive]);

  return {
    lines,
    setLines,
    loading,
    error,
    loadingOlder,
    hasOlderLogs,
    loadingNewer,
    hasNewerLogs,
    following,
    atTop,
    setFollowing,
    live,
    liveStatus,
    toggleLive,
    stopLive,
    resumeLive,
    containerRef,
    fetchLogs,
    loadOlder,
    loadNewer,
    handleScroll,
  };
}
