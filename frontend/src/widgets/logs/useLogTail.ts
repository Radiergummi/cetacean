import type { LogLine as WireLogLine } from "@/api/client";
import { type LogLine, maxLiveLines, toLogLine } from "@/components/log/log-utils";
import { useEffect, useRef, useState } from "react";

/**
 * How long the widget waits between reads.
 *
 * A widget cannot hold Cetacean's SSE tail open — a sandboxed iframe has no
 * route to the HTTP API, and tools/call through the host is the only sanctioned
 * data path — so the tail is polled. Five seconds is slow enough to cost the
 * host little and fast enough to read as live.
 */
export const pollIntervalMs = 5_000;

/**
 * The arguments the host called `get_logs` with, as the widget replays them.
 * The widget shows exactly the call the model made, narrowed interactively.
 */
export interface LogTailArguments {
  service: string;
  tail?: number | undefined;
  level?: string | undefined;
}

/** The `get_logs` structured output, mirroring internal/mcp.LogResourceResponse. */
interface GetLogsResult {
  lines?: WireLogLine[];
  cursor?: string;
}

interface LogTailState {
  lines: LogLine[];
  error: Error | undefined;
  isLoading: boolean;
}

/**
 * Tails a service's logs by re-reading `get_logs` from the cursor it returns.
 *
 * The cursor is the server's, never the widget's: Docker ignores `since` for
 * service logs, so the window is narrowed after parsing in internal/logs and
 * only the server knows which lines a caller has already seen. Re-deriving that
 * here — from the newest timestamp on screen, say — would reintroduce the very
 * gap `FilterSince` exists to close.
 *
 * Each read is scheduled after the previous one settles rather than on a fixed
 * interval, so a slow or failing host cannot pile requests up behind itself.
 */
export function useLogTail(
  callTool: <T>(name: string, args?: Record<string, unknown>) => Promise<T>,
  args: LogTailArguments | undefined,
): LogTailState {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [error, setError] = useState<Error | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(true);

  // Line indices identify a row to the table and must stay unique for the life
  // of the widget, so they are counted rather than derived from the array —
  // which is trimmed at maxLiveLines and would start repeating.
  const nextIndex = useRef(0);

  // Serialised so a caller can pass an object literal without restarting the
  // tail on every render.
  const argsKey = JSON.stringify(args ?? null);

  useEffect(() => {
    const parsed = JSON.parse(argsKey) as LogTailArguments | null;

    if (!parsed) {
      return;
    }

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let cursor: string | undefined;

    async function read() {
      try {
        const result = await callTool<GetLogsResult>("get_logs", {
          ...parsed,
          ...(cursor ? { since: cursor } : {}),
        });

        if (cancelled) {
          return;
        }

        cursor = result.cursor ?? cursor;

        const fresh = (result.lines ?? []).map((line) => toLogLine(line, nextIndex.current++));

        if (fresh.length > 0) {
          setLines((previous) => {
            const merged = [...previous, ...fresh];

            return merged.length > maxLiveLines ? merged.slice(-maxLiveLines) : merged;
          });
        }

        setError(undefined);
      } catch (cause) {
        if (cancelled) {
          return;
        }

        setError(cause instanceof Error ? cause : new Error(String(cause)));
      } finally {
        // A failed read is not a dead tail: the service may be restarting, or
        // the host may have dropped one call. Keep reading and let the error
        // clear itself when the next one lands.
        if (!cancelled) {
          setIsLoading(false);
          timer = setTimeout(() => void read(), pollIntervalMs);
        }
      }
    }

    void read();

    return () => {
      cancelled = true;

      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [callTool, argsKey]);

  return { lines, error, isLoading };
}
