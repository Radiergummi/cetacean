import { useLatestRef } from "../hooks/useLatestRef";
import { useEffect, useRef } from "react";

/**
 * Calls `onLoadMore` when scrolled into view, for layouts without DataTable's
 * own sentinel. Remount it whenever the list grows (a `key` on its length), so
 * a sentinel still in view after a page lands asks for the next one.
 */
export default function LoadMoreSentinel({ onLoadMore }: { onLoadMore: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const onLoadMoreRef = useLatestRef(onLoadMore);

  useEffect(() => {
    if (!ref.current) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some(({ isIntersecting }) => isIntersecting)) {
          onLoadMoreRef.current();
        }
      },
      { rootMargin: "200px" },
    );

    observer.observe(ref.current);

    return () => observer.disconnect();
  }, [onLoadMoreRef]);

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
