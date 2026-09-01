import type { LogLine } from "@/components/log/log-utils";
import { LogTable } from "@/components/log/LogTable";
import { LevelFilter } from "@/components/log/LogToolbar";
import { useLogFilter } from "@/components/log/useLogFilter";
import { useEffect, useRef, useState } from "react";

interface Props {
  /** The service being tailed, named in the empty state so the reader knows. */
  service: string;
  lines: LogLine[];
  error?: Error | undefined;
}

/** How close to the bottom counts as following the tail, in pixels. */
const followThreshold = 40;

/**
 * The presentational half of the log widget: filter, follow, and render.
 *
 * Split from LogWidget so it can be exercised without a host bridge — the host
 * side is one polling hook, and everything a reviewer would want to try (does
 * the level filter narrow? does search?) lives here.
 *
 * Filtering is client-side over the lines already fetched, and deliberately
 * separate from the `level` argument the model passed to `get_logs`: that one
 * chose which lines the widget receives at all, this one narrows what is shown
 * without spending a round trip through the host on a keystroke.
 */
export function LogTail({ error, lines, service }: Props) {
  const { caseSensitive, filtered, levelFilter, search, setLevelFilter, setSearch, useRegex } =
    useLogFilter(lines);

  const containerRef = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(true);

  // Scrolling away from the bottom pauses the follow, and scrolling back
  // resumes it — the same bargain the dashboard's viewer strikes, so reading
  // back through a busy service is not fought by every arriving line.
  function handleScroll() {
    const container = containerRef.current;

    if (!container) {
      return;
    }

    const distance = container.scrollHeight - container.scrollTop - container.clientHeight;

    setFollowing(distance < followThreshold);
  }

  useEffect(() => {
    const container = containerRef.current;

    if (!following || !container) {
      return;
    }

    container.scrollTop = container.scrollHeight;
  }, [filtered.length, following]);

  return (
    <div className="flex h-full flex-col gap-2 p-2">
      <div className="flex items-center gap-2">
        <input
          type="search"
          value={search}
          onChange={({ target }) => setSearch(target.value)}
          placeholder={`Search ${service} logs…`}
          className="h-8 min-w-0 flex-1 rounded-md border bg-background px-2 text-xs"
        />

        <LevelFilter
          value={levelFilter}
          onChange={setLevelFilter}
        />

        <span className="text-xs whitespace-nowrap opacity-70">
          {filtered.length === lines.length
            ? `${lines.length} lines`
            : `${filtered.length} of ${lines.length} lines`}
        </span>
      </div>

      {error && <p className="text-xs text-red-600 dark:text-red-300">{error.message}</p>}

      {lines.length === 0 && !error ? (
        <p className="p-3 text-sm opacity-70">Waiting for output from {service}…</p>
      ) : (
        <LogTable
          containerRef={containerRef}
          handleScroll={handleScroll}
          filtered={filtered}
          showAttrs={false}
          wrapLines={false}
          search={search}
          caseSensitive={caseSensitive}
          useRegex={useRegex}
          following={following}
          fillHeight
        />
      )}
    </div>
  );
}
