import { useCallback, useEffect, useRef, useState } from "react";
import type { LogLine as ApiLogLine } from "../../api/client";
import { api } from "../../api/client";
import type { LogLine, TimeRange } from "./log-utils";
import { MAX_LIVE_LINES, toLogLine } from "./log-utils";

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

  const limit = 500;
  const oldestRef = useRef<string | undefined>(undefined);
  const newestRef = useRef<string | undefined>(undefined);
  const containerRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<{ abort(): void } | null>(null);
  const scrollRafRef = useRef(0);

  const streamParam = streamFilter === "all" ? undefined : streamFilter;

  const fetchLogs = useCallback(() => {
    abortRef.current?.abort();
    setLoading(true);
    setError(null);
    setHasOlderLogs(true);
    setHasNewerLogs(false);
    oldestRef.current = undefined;
    newestRef.current = undefined;

    const controller = new AbortController();
    abortRef.current = controller;
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
        const newLines = response.lines?.map(toLogLine) ?? [];
        setLines(newLines);
        oldestRef.current = response.oldest;
        newestRef.current = response.newest;
        setHasOlderLogs(response.hasMore ?? newLines.length >= limit);
        setLoading(false);
      })
      .catch((caught) => {
        if (controller.signal.aborted && !timedOut) {
          // Aborted (e.g. by React StrictMode remount) — don't update state,
          // the next fetch will handle it.
          return;
        }
        if (timedOut) {
          setError("Request timed out");
        } else {
          setError(caught instanceof Error ? caught.message : "Failed to load logs");
        }
        setLoading(false);
      })
      .finally(() => clearTimeout(timeout));
  }, [logId, isTask, limit, timeRange, streamParam]);

  useEffect(() => {
    fetchLogs();
    return () => {
      abortRef.current?.abort();
    };
  }, [fetchLogs]);

  // Live streaming via SSE
  useEffect(() => {
    if (!live) return;

    const after = newestRef.current || new Date().toISOString();
    const streamOptions = { after, stream: streamParam };
    const url = isTask
      ? api.taskLogsStreamURL(logId, streamOptions)
      : api.serviceLogsStreamURL(logId, streamOptions);

    const eventSource = new EventSource(url);
    abortRef.current = { abort: () => eventSource.close() };
    const buffer: ApiLogLine[] = [];
    let animationFrameId = 0;

    const flush = () => {
      animationFrameId = 0;
      if (buffer.length === 0) return;
      const batch = buffer.splice(0);
      setLines((current) => {
        const updated = current.concat(
          batch.map((line, index) => toLogLine(line, current.length + index)),
        );
        return updated.length > MAX_LIVE_LINES ? updated.slice(-MAX_LIVE_LINES) : updated;
      });
    };

    eventSource.onmessage = (event) => {
      try {
        buffer.push(JSON.parse(event.data));
        if (!animationFrameId) animationFrameId = requestAnimationFrame(flush);
      } catch {
        /* skip malformed events */
      }
    };

    eventSource.onerror = () => {};

    return () => {
      eventSource.close();
      cancelAnimationFrame(animationFrameId);
      abortRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live, logId, isTask, streamParam]);

  // Auto-scroll to bottom when following and lines change.
  // On initial mount the virtualizer handles scrolling via initialOffset.
  // This effect covers the non-virtual case and subsequent updates.
  // The rAF is stored in a ref so React's effect cleanup can't cancel it.
  useEffect(() => {
    if (!following || !containerRef.current) return;
    const node = containerRef.current;
    cancelAnimationFrame(scrollRafRef.current);
    scrollRafRef.current = requestAnimationFrame(() => {
      node.scrollTop = node.scrollHeight;
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
          requestAnimationFrame(() => {
            if (scrollElement)
              scrollElement.scrollTop += scrollElement.scrollHeight - previousScrollHeight;
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

  // When not live, check once for newer log availability after initial load.
  useEffect(() => {
    if (live || !newestRef.current || loading) return;
    const cursor = newestRef.current;
    if (!cursor) return;
    const options = { limit: 1, after: cursor, stream: streamParam };
    const request = isTask ? api.taskLogs(logId, options) : api.serviceLogs(logId, options);
    request
      .then((response) => {
        setHasNewerLogs((response.lines?.length ?? 0) > 0);
      })
      .catch(() => {});
  }, [live, loading, logId, isTask, streamParam]);

  const handleScroll = useCallback(() => {
    const element = containerRef.current;
    if (!element) return;
    const atBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 50;
    setFollowing(atBottom);
    setAtTop(element.scrollTop < 50);
  }, []);

  const toggleLive = useCallback(() => {
    if (live) {
      abortRef.current?.abort();
      setLive(false);
    } else {
      setFollowing(true);
      setLive(true);
    }
  }, [live]);

  const stopLive = useCallback(() => {
    abortRef.current?.abort();
    setLive(false);
  }, []);

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
    toggleLive,
    stopLive,
    containerRef,
    fetchLogs,
    loadOlder,
    loadNewer,
    handleScroll,
  };
}
