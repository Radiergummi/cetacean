import { useLoadMoreObserver } from "../hooks/useLoadMoreObserver";
import { useRef } from "react";

/**
 * Calls `onLoadMore` when scrolled into view, for layouts without DataTable's
 * own sentinel. Remount it whenever the list grows (a `key` on its length), so
 * a sentinel still in view after a page lands asks for the next one.
 */
export default function LoadMoreSentinel({ onLoadMore }: { onLoadMore: () => void }) {
  const ref = useRef<HTMLDivElement>(null);

  useLoadMoreObserver(ref, onLoadMore);

  return (
    <div
      ref={ref}
      data-testid="load-more-sentinel"
      className="p-3 text-center text-sm text-muted-foreground"
    >
      Loading…
    </div>
  );
}
